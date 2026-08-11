package httpd

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"sync"

	"github.com/jumpserver/koko/pkg/logger"
	"github.com/jumpserver/koko/pkg/session"
)

var _ Handler = (*webSftp)(nil)

type webSftp struct {
	ws *UserWebsocket

	done chan struct{}

	volume *UserWebVolume

	stateMu        sync.Mutex
	started        bool
	trackSessionID bool

	// 底层 chunkFilesMap/ftpLogMap 使用 int key(历史遗留,且受 elfinder.Volume 接口约束)。
	// 前端消息 id 是字符串(新 client 用 UUID),这里在 handler 内维护 id->int 映射,
	// 保证同一次上传的多个分片与最终 merge 命中同一个 key,不同上传之间不冲突。
	// int 底层与 elfinder 路径的行为保持不变。
	uploadKeyMu  sync.Mutex
	uploadKeySeq int
	uploadKeys   map[string]int
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
	return nil
}

func (h *webSftp) HandleMessage(msg *Message) {
	go h.dispatch(*msg)
}

func (h *webSftp) CleanUp() {
	close(h.done)
	h.volume.Close()
}

// resolveUploadKey 将前端消息 id(可能是 UUID)映射为一个稳定的 int chunk key。
// 同一个 msg.Id 多次调用返回同一个 int,保证分片上传与 merge 命中同一底层 chunk。
func (h *webSftp) resolveUploadKey(msgID string) int {
	h.uploadKeyMu.Lock()
	defer h.uploadKeyMu.Unlock()
	if h.uploadKeys == nil {
		h.uploadKeys = make(map[string]int)
	}
	if key, ok := h.uploadKeys[msgID]; ok {
		return key
	}
	h.uploadKeySeq++
	h.uploadKeys[msgID] = h.uploadKeySeq
	return h.uploadKeySeq
}

// releaseUploadKey 在一次上传结束(merge/单文件完成)后清理映射,避免长连接下累积。
func (h *webSftp) releaseUploadKey(msgID string) {
	h.uploadKeyMu.Lock()
	defer h.uploadKeyMu.Unlock()
	delete(h.uploadKeys, msgID)
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
	h.stateMu.Lock()
	started := h.started
	trackSessionID := h.trackSessionID
	if !started {
		h.started = true
		if h.ws.ConnectToken != nil {
			trackSessionID = !notInTokenIds(h.ws.ConnectToken.Id)
			h.trackSessionID = trackSessionID
		}
	}
	h.stateMu.Unlock()
	if started && trackSessionID && (h.ws.ConnectToken == nil || notInTokenIds(h.ws.ConnectToken.Id)) {
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

func (h *webSftp) handleList(request *webSftpRequest, response *Message) {
	response.Data, response.CurrentPath = h.list(request.Path)
	h.ws.SendMessage(response)
}

func (h *webSftp) list(path string) (string, string) {
	files := h.volume.List(path)
	data, _ := json.Marshal(files)
	return string(data), h.volume.UserSftp.GetCurrentPath()
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

	var err error
	if request.Merge {
		id := h.resolveUploadKey(msg.Id)
		err = h.volume.MergeChunk(id, request.Path)
		h.releaseUploadKey(msg.Id)
		response.Data = "ok"
	} else if request.Chunk {
		id := h.resolveUploadKey(msg.Id)
		err = h.volume.UploadChunk(id, request.Path, request.OffSet, int64(reader.Len()), readerAt)
		response.Data = request.Path
	} else {
		// 新建文件/单块上传:不使用 chunk key,直接写入
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
