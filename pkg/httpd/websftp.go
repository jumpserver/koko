package httpd

import (
	"bytes"
	"encoding/json"
	"errors"
	"github.com/jumpserver/koko/pkg/logger"
	"github.com/jumpserver/koko/pkg/session"
	"io"
	"strconv"
	"time"
)

var _ Handler = (*webSftp)(nil)

type webSftp struct {
	ws *UserWebsocket

	done chan struct{}

	volume *UserWebVolume

	currentPath string

	isConnected bool

	msg *Message
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
	go h.monitor()
	return nil
}

func (h *webSftp) HandleMessage(msg *Message) {
	h.msg = msg
	switch msg.Cmd {
	case "cancel":
		// TODO Interrupt download
	default:
		go h.dispatch()
	}
}

func (h *webSftp) CleanUp() {
	close(h.done)
	h.volume.Close()
}

func (h *webSftp) monitor() {
	for {
		if !h.isConnected {
			continue
		}

		if err := h.checkSession(); err != nil {
			h.sendError("closed", err.Error())
			return
		}

		time.Sleep(time.Second * 30)
	}

}

func (h *webSftp) checkSession() error {
	_, ok := session.GetSessionById(h.ws.ConnectToken.Id)
	if !ok {
		return errors.New("session closed")
	}
	return nil
}

func (h *webSftp) sendError(tp, err string) {
	logger.Errorf("webSftp error: %v", err)
	h.ws.SendMessage(&Message{
		Id:   h.msg.Id,
		Cmd:  h.msg.Cmd,
		Type: tp,
		Err:  err,
	})
}

type webSftpRequest struct {
	Path    string `json:"path"`
	NewName string `json:"new_name"`
	Chunk   bool   `json:"chunk"`
	Merge   bool   `json:"merge"`
	OffSet  int64  `json:"offset"`
	Size    int64  `json:"size"`
	IsDir   bool   `json:"is_dir"`
}

func (h *webSftp) dispatch() {
	message := Message{
		Id:          h.msg.Id,
		Cmd:         h.msg.Cmd,
		Type:        SFTPData,
		CurrentPath: h.currentPath,
	}

	request := &webSftpRequest{}
	err := json.Unmarshal([]byte(h.msg.Data), request)
	if err != nil {
		message.Err = err.Error()
		h.ws.SendMessage(&message)
		return
	}

	if h.isConnected {
		if err := h.checkSession(); err != nil {
			h.sendError("closed", err.Error())
			return
		}
	}

	switch h.msg.Cmd {
	case "list":
		h.handleList(request, &message)
	case "download":
		h.handleDownload(request, &message)
	case "upload":
		h.handleUpload(request, h.msg, &message)
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

	h.isConnected = true

}

func (h *webSftp) handleList(request *webSftpRequest, response *Message) {
	response.Data = h.list(request.Path)
	response.CurrentPath = h.currentPath
	h.ws.SendMessage(response)
}

func (h *webSftp) list(path string) string {
	files := h.volume.List(path)
	h.currentPath = h.volume.UserSftp.GetCurrentPath()
	data, _ := json.Marshal(files)
	return string(data)
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
		err = h.volume.UploadFile(request.Path, readerAt, request.Size)
		response.Data = request.Path
	}
	if err != nil {
		response.Err = err.Error()
		h.ws.SendMessage(response)
		return
	}
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
