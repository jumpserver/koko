package httpd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"

	"github.com/jumpserver/koko/internal/sessiontools"
	"github.com/jumpserver/koko/pkg/i18n"
)

var _ Handler = (*webSftp)(nil)

type webSftp struct {
	ws *UserWebsocket

	done chan struct{}

	volume            *sftpVolume
	mcp               *sessiontools.MCPDispatcher
	resourceSessionID string

	stateMu  sync.Mutex
	ready    chan struct{}
	closed   bool
	expired  atomic.Bool
	requests chan struct{}
	pending  sync.WaitGroup
}

func newWebSFTP(ws *UserWebsocket) *webSftp {
	return &webSftp{
		ws: ws, done: make(chan struct{}), ready: make(chan struct{}),
		requests: make(chan struct{}, 4),
	}
}

func (h *webSftp) CheckValidation() error {
	volume, err := newWebSFTPVolume(h.ws)
	if err != nil {
		return err
	}
	h.volume = volume
	volume.conn.SetOnSessionClosed(func() {
		h.expired.Store(true)
		h.ws.SendMessage(&Message{Id: h.ws.Uuid, Type: CLOSE, Err: i18n.NewLang(h.ws.langCode).T("FileManagementExpired")})
	})
	h.initializeFileTools()
	close(h.ready)
	return nil
}

func (h *webSftp) HandleMessage(msg *Message) {
	select {
	case <-h.ready:
	case <-h.done:
		return
	case <-h.ws.done:
		return
	}
	if msg.Type == TerminalBinary {
		parsed, err := parseSftpBinaryFrame(msg.Raw)
		if err != nil {
			h.ws.SendMessage(&Message{Type: ERROR, Err: err.Error()})
			return
		}
		msg = parsed
	}
	if msg.Type == MCPRequest || msg.Type == MCPCancel {
		h.handleFileToolMessage(msg)
		return
	}
	// Apply backpressure before spawning; at most four file requests run at once.
	select {
	case h.requests <- struct{}{}:
	case <-h.done:
		return
	case <-h.ws.done:
		return
	}
	h.stateMu.Lock()
	if h.closed {
		h.stateMu.Unlock()
		<-h.requests
		return
	}
	h.pending.Add(1)
	h.stateMu.Unlock()
	go func() {
		defer h.pending.Done()
		defer func() { <-h.requests }()
		h.dispatch(*msg)
	}()
}

func (h *webSftp) CleanUp() {
	h.stateMu.Lock()
	if h.closed {
		h.stateMu.Unlock()
		return
	}
	h.closed = true
	close(h.done)
	dispatcher := h.mcp
	h.stateMu.Unlock()
	if h.volume != nil {
		h.volume.Close()
	}
	if dispatcher != nil {
		dispatcher.Close()
	}
	h.pending.Wait()
}

func (h *webSftp) sessionExpired() bool { return h.expired.Load() }

func (h *webSftp) requestContext() context.Context { return h.ws.ctx.Request.Context() }

func (h *webSftp) WebsocketCapabilities() map[string]any {
	var readAllowed, writeAllowed bool
	if h.ws.ConnectToken != nil {
		readAllowed = h.ws.ConnectToken.Actions.EnableDownload()
		writeAllowed = h.ws.ConnectToken.Actions.EnableUpload()
	}

	return map[string]any{
		"web_sftp": webSftpCapabilities{
			SchemaVersion:  1,
			TransferBinary: true,
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
	Binary          bool    `json:"binary"`
}

type webSftpCapabilities struct {
	SchemaVersion  int                         `json:"schema_version"`
	TransferBinary bool                        `json:"transfer_binary"`
	FileEditor     webSftpFileEditorCapability `json:"file_editor"`
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
		message.Err = i18n.NewLang(h.ws.langCode).T("FileManagementExpired")
		message.Type = CLOSE
		h.ws.SendMessage(&message)
		return
	}
	if err := h.checkPermission(msg.Cmd); err != nil {
		h.sendError(&message, err)
		return
	}
	switch msg.Cmd {
	case "list":
		h.handleList(request, &message)
	case "download":
		h.handleDownload(request, &message)
	case "upload":
		h.handleUpload(request, &msg, &message)
	case "transfer_read":
		h.handleTransferRead(request, &message)
	case "transfer_prepare", "transfer_write", "transfer_status", "transfer_commit", "transfer_cancel":
		h.handleTransferMutation(request, &msg, &message)
	case "save":
		h.handleSave(request, &msg, &message)
	case "rm":
		h.handleAction(h.rm, request, &message)
	case "rename":
		h.handleAction(h.rename, request, &message)
	case "mkdir":
		h.handleAction(h.mkdir, request, &message)
	default:
		h.sendError(&message, fmt.Errorf("Unknown command"))
	}
}

func (h *webSftp) checkPermission(command string) error {
	token := h.ws.ConnectToken
	if token == nil {
		return nil
	} // Per-account permissions are checked by UserSftpConn.
	allowed := true
	switch command {
	case "download", "transfer_read":
		allowed = token.Actions.EnableDownload()
	case "upload", "save", "mkdir", "rename", "transfer_prepare", "transfer_write", "transfer_status", "transfer_commit", "transfer_cancel":
		allowed = token.Actions.EnableUpload()
	case "rm":
		allowed = token.Actions.EnableDelete()
	}
	return requireFilePermission(allowed)
}

func (h *webSftp) sendError(response *Message, err error) {
	response.Err = err.Error()
	switch {
	case errors.Is(err, ErrWebSftpFileConflict):
		response.ErrorCode = "sftp_file_conflict"
	case errors.Is(err, os.ErrExist):
		response.ErrorCode = "sftp_file_exists"
	case isSftpNotExist(err):
		response.ErrorCode = "sftp_path_not_found"
	}
	h.ws.SendMessage(response)
}

func (h *webSftp) handleList(request *webSftpRequest, response *Message) {
	files, currentPath, err := h.volume.List(request.Path)
	data, _ := json.Marshal(files)
	response.Data, response.CurrentPath = string(data), currentPath
	if err != nil {
		h.sendError(response, err)
		return
	}
	h.ws.SendMessage(response)
}

func (h *webSftp) handleDownload(request *webSftpRequest, response *Message) {
	file, filename, err := h.volume.Download(request.Path, request.IsDir)
	if err != nil {
		h.sendError(response, err)
		return
	}

	defer file.Close()

	if err := h.streamFileContent(file, response); err != nil {
		h.sendError(response, err)
		return
	}
	response.Data = filename
	response.Type = SFTPData
	h.ws.SendMessage(response)
}

func (h *webSftp) streamFileContent(reader io.Reader, response *Message) error {
	buf := make([]byte, transferChunkMaxSize)
	for {
		select {
		case <-h.done:
			return context.Canceled
		case <-h.ws.done:
			return context.Canceled
		default:
		}
		n, err := reader.Read(buf)
		if n > 0 {
			part := *response
			part.Type = SFTPBinary
			part.Raw = append([]byte(nil), buf[:n]...)
			h.ws.SendMessage(&part)
		}
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

func (h *webSftp) handleUpload(request *webSftpRequest, msg *Message, response *Message) {
	reader := bytes.NewReader(msg.Raw)

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
		err = h.volume.UploadChunk(id, request.Path, request.OffSet, int64(reader.Len()), reader)
		response.Data = request.Path
	} else {
		err = h.volume.UploadFile(request.Path, reader, request.Size)
		response.Data = "ok"
	}
	if err != nil {
		h.sendError(response, err)
		return
	}
	h.ws.SendMessage(response)
}

func (h *webSftp) handleSave(request *webSftpRequest, msg *Message, response *Message) {
	entry, err := h.volume.SaveFile(
		h.requestContext(), request.Path,
		bytes.NewReader(msg.Raw),
		request.Size,
		fileMutationOptions{expectedVersion: request.ExpectedVersion, force: request.Force},
	)
	if err != nil {
		h.sendError(response, err)
		return
	}
	data, _ := json.Marshal(entry)
	response.Data = string(data)
	h.ws.SendMessage(response)
}

func (h *webSftp) handleAction(action func(*webSftpRequest) error, request *webSftpRequest, response *Message) {
	err := action(request)
	if err != nil {
		h.sendError(response, err)
		return
	}
	response.Data = "ok"
	h.ws.SendMessage(response)
}

func (h *webSftp) rm(request *webSftpRequest) error {
	return h.volume.Remove(h.requestContext(), request.Path, true, fileMutationOptions{})
}

func (h *webSftp) rename(request *webSftpRequest) error {
	oldNamePath := request.Path
	newName := request.NewName
	return h.volume.Rename(h.requestContext(), oldNamePath, filepath.Join(filepath.Dir(oldNamePath), newName), fileMutationOptions{})
}

func (h *webSftp) mkdir(request *webSftpRequest) error {
	return h.volume.MakeDir(h.requestContext(), request.Path, fileMutationOptions{})
}
