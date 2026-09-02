package httpd

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strconv"
	"sync"

	"github.com/jumpserver/koko/internal/sessiontools"
	"github.com/jumpserver/koko/pkg/logger"
	"github.com/jumpserver/koko/pkg/session"
)

var _ Handler = (*webSftp)(nil)

type webSftp struct {
	ws *UserWebsocket

	done chan struct{}

	volume            *UserWebVolume
	mcp               *sessiontools.MCPDispatcher
	resourceSessionID string

	stateMu        sync.Mutex
	started        bool
	trackSessionID bool
}

func (h *webSftp) Name() string {
	return WebFolderName
}

func (h *webSftp) CheckValidation() error {
	volume, err := SftpCheckValidation(h.ws)
	if err != nil {
		return err
	}

	h.volume = NewUserWebVolume(volume)
	h.initializeFileTools()
	return nil
}

func (h *webSftp) HandleMessage(msg *Message) {
	if msg.Type == MCPRequest || msg.Type == MCPCancel {
		h.handleFileToolMessage(msg)
		return
	}
	go h.dispatch(*msg)
}

func (h *webSftp) CleanUp() {
	close(h.done)
	dispatcher := h.getMCP()
	var closeVolume func()
	if h.volume != nil {
		closeVolume = h.volume.Close
	}
	if dispatcher != nil {
		dispatcher.Close()
	}
	if closeVolume != nil {
		closeVolume()
	}
}

func (h *webSftp) WebsocketCapabilities() map[string]any {
	var readAllowed, writeAllowed bool
	if h.ws.ConnectToken != nil {
		readAllowed = h.ws.ConnectToken.Actions.EnableDownload()
		writeAllowed = h.ws.ConnectToken.Actions.EnableUpload()
	}

	return map[string]any{
		"web_sftp": webSftpCapabilities{
			SchemaVersion: 1,
			FileEditor: webSftpFileEditorCapability{
				Enabled: readAllowed && writeAllowed,
				Read:    readAllowed,
				Write:   writeAllowed,
				Save: webSftpSaveCapability{
					Version:         1,
					ExpectedVersion: true,
					Force:           true,
					MaxBytes:        maxWebEditorFileSize,
				},
			},
		},
	}
}

func (h *webSftp) setMCP(dispatcher *sessiontools.MCPDispatcher) {
	h.stateMu.Lock()
	h.mcp = dispatcher
	h.stateMu.Unlock()
}

func (h *webSftp) getMCP() *sessiontools.MCPDispatcher {
	h.stateMu.Lock()
	defer h.stateMu.Unlock()
	return h.mcp
}

type webSftpRequest struct {
	Path            string  `json:"path"`
	NewName         string  `json:"new_name"`
	Chunk           bool    `json:"chunk"`
	Merge           bool    `json:"merge"`
	OffSet          int64   `json:"offset"`
	Size            int64   `json:"size"`
	IsDir           bool    `json:"is_dir"`
	ExpectedVersion *string `json:"expected_version"`
	Force           bool    `json:"force"`
	TransferID      string  `json:"transfer_id"`
	Length          int64   `json:"length"`
	SHA256          string  `json:"sha256"`
	ConflictPolicy  string  `json:"conflict_policy"`
	Discard         bool    `json:"discard"`
}

type webSftpCapabilities struct {
	SchemaVersion int                         `json:"schema_version"`
	FileEditor    webSftpFileEditorCapability `json:"file_editor"`
}

type webSftpFileEditorCapability struct {
	Enabled bool                  `json:"enabled"`
	Read    bool                  `json:"read"`
	Write   bool                  `json:"write"`
	Save    webSftpSaveCapability `json:"save"`
}

type webSftpSaveCapability struct {
	Version         int   `json:"version"`
	ExpectedVersion bool  `json:"expected_version"`
	Force           bool  `json:"force"`
	MaxBytes        int64 `json:"max_bytes"`
}

func notInTokenIds(target string) bool {
	for _, item := range session.GetAliveSessionTokenIds() {
		if item == target {
			return false
		}
	}
	return true
}

func (h *webSftp) dispatch(msg Message) {
	message := Message{
		Id:   msg.Id,
		Cmd:  msg.Cmd,
		Type: SFTPData,
	}

	request := &webSftpRequest{}
	err := json.Unmarshal([]byte(msg.Data), request)
	if err != nil {
		message.Err = err.Error()
		h.ws.SendMessage(&message)
		return
	}
	if h.sessionExpired() {
		message.Err = "Session expired or not found"
		message.Type = CLOSE
		h.ws.SendMessage(&message)
		return
	}
	switch msg.Cmd {
	case "list":
		h.handleList(request, &message)
	case "download":
		if h.ws.ConnectToken.Actions.EnableDownload() {
			h.handleDownload(request, &message)
		} else {
			message.Err = "Permission denied"
			h.ws.SendMessage(&message)
			return
		}

	case "upload":
		if h.ws.ConnectToken.Actions.EnableUpload() {
			h.handleUpload(request, &msg, &message)
		} else {
			message.Err = "Permission denied"
			h.ws.SendMessage(&message)
			return
		}
	case "transfer_read":
		if h.ws.ConnectToken.Actions.EnableDownload() {
			h.handleTransferRead(request, &message)
		} else {
			message.Err = "Permission denied"
			h.ws.SendMessage(&message)
			return
		}
	case "transfer_prepare", "transfer_write", "transfer_status", "transfer_commit", "transfer_cancel":
		if h.ws.ConnectToken.Actions.EnableUpload() {
			h.handleTransferMutation(request, &msg, &message)
		} else {
			message.Err = "Permission denied"
			h.ws.SendMessage(&message)
			return
		}

	case "save":
		if h.ws.ConnectToken.Actions.EnableUpload() {
			h.handleSave(request, &msg, &message)
		} else {
			message.Err = "Permission denied"
			h.ws.SendMessage(&message)
			return
		}

	case "rm":
		h.handleAction(h.rm, request, &message)
	case "rename":
		h.handleAction(h.rename, request, &message)
	case "mkdir":
		h.handleAction(h.mkdir, request, &message)
	default:
		message.Err = "Unknown command"
		h.ws.SendMessage(&message)
	}

}

func (h *webSftp) sessionExpired() bool {
	h.stateMu.Lock()
	defer h.stateMu.Unlock()
	started := h.started
	if !started {
		h.started = true
		if h.ws.ConnectToken != nil {
			h.trackSessionID = !notInTokenIds(h.ws.ConnectToken.Id)
		}
	}
	return started && h.trackSessionID &&
		(h.ws.ConnectToken == nil || notInTokenIds(h.ws.ConnectToken.Id))
}

func (h *webSftp) trackedSessionExpired() bool {
	h.stateMu.Lock()
	defer h.stateMu.Unlock()
	return h.started && h.trackSessionID &&
		(h.ws.ConnectToken == nil || notInTokenIds(h.ws.ConnectToken.Id))
}

func (h *webSftp) handleList(request *webSftpRequest, response *Message) {
	var err error
	response.Data, response.CurrentPath, err = h.list(request.Path)
	if err != nil {
		response.Err = err.Error()
	}
	h.ws.SendMessage(response)
}

func (h *webSftp) list(path string) (string, string, error) {
	files, currentPath, err := h.volume.List(path)
	data, _ := json.Marshal(files)
	return string(data), currentPath, err
}

func (h *webSftp) handleDownload(request *webSftpRequest, response *Message) {
	file, filename, err := h.volume.Download(request.Path, request.IsDir)
	if err != nil {
		response.Err = err.Error()
		h.ws.SendMessage(response)
		return
	}

	if file.Reader != nil {
		defer file.Reader.Close()
	}

	h.streamFileContent(file, response)
	response.Data = filename
	response.Type = SFTPData
	h.ws.SendMessage(response)
}

func (h *webSftp) streamFileContent(file FileData, response *Message) {
	response.Type = SFTPBinary
	buf := make([]byte, 1024*1024*2)
	for {
		responseCopy := *response
		n, err := file.Reader.Read(buf)
		if err != nil {
			if err != io.EOF {
				logger.Errorf("Error reading file: %s", err)
				responseCopy.Err = err.Error()
				h.ws.SendMessage(&responseCopy)
			}
			responseCopy.Raw = append([]byte{}, buf[:n]...)
			h.ws.SendMessage(&responseCopy)
			return
		}

		responseCopy.Raw = append([]byte{}, buf[:n]...)
		h.ws.SendMessage(&responseCopy)
	}
}

func (h *webSftp) handleUpload(request *webSftpRequest, msg *Message, response *Message) {
	reader := bytes.NewReader(msg.Raw)
	var readerAt io.ReaderAt = reader

	id, idErr := strconv.Atoi(msg.Id)
	if idErr != nil {
		response.Err = idErr.Error()
		h.ws.SendMessage(response)
		return
	}
	var err error
	if request.Merge {
		err = h.volume.MergeChunk(id, request.Path)
		response.Data = "ok"
	} else if request.Chunk {
		err = h.volume.UploadChunk(id, request.Path, request.OffSet, int64(reader.Len()), readerAt)
		response.Data = request.Path
	} else {
		err = h.volume.UploadFile(request.Path, reader, request.Size)
		response.Data = "ok"
	}
	if err != nil {
		response.Err = err.Error()
		h.ws.SendMessage(response)
		return
	}
	h.ws.SendMessage(response)
}

func (h *webSftp) handleSave(request *webSftpRequest, msg *Message, response *Message) {
	entry, err := h.volume.SaveFile(
		request.Path,
		bytes.NewReader(msg.Raw),
		request.Size,
		request.ExpectedVersion,
		request.Force,
	)
	if err != nil {
		response.Err = err.Error()
		if errors.Is(err, ErrWebSftpFileConflict) {
			response.ErrorCode = "sftp_file_conflict"
		}
		h.ws.SendMessage(response)
		return
	}
	data, _ := json.Marshal(entry)
	response.Data = string(data)
	h.ws.SendMessage(response)
}

func (h *webSftp) handleAction(action func(*webSftpRequest) error, request *webSftpRequest, response *Message) {
	err := action(request)
	if err != nil {
		response.Err = err.Error()
	} else {
		response.Data = "ok"
	}
	h.ws.SendMessage(response)
}

func (h *webSftp) rm(request *webSftpRequest) error {
	return h.volume.Remove(request.Path)
}

func (h *webSftp) rename(request *webSftpRequest) error {
	oldNamePath := request.Path
	newName := request.NewName
	return h.volume.Rename(oldNamePath, newName)
}

func (h *webSftp) mkdir(request *webSftpRequest) error {
	return h.volume.MakeDir(request.Path)
}
