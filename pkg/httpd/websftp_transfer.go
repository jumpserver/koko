package httpd

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"strings"

	"github.com/pkg/sftp"
)

const transferChunkMaxSize = 2 * 1024 * 1024

type webSftpTransferResponse struct {
	TransferID     string `json:"transfer_id"`
	CommittedBytes int64  `json:"committed_bytes"`
	TotalBytes     int64  `json:"total_bytes"`
	State          string `json:"state"`
	Duplicate      bool   `json:"duplicate,omitempty"`
}

type webSftpTransferChunk struct {
	Offset int64  `json:"offset"`
	SHA256 string `json:"sha256"`
	EOF    bool   `json:"eof"`
}

func transferStagePath(targetPath, transferID string) (string, error) {
	if targetPath == "" || transferID == "" || len(transferID) > 128 {
		return "", fmt.Errorf("invalid file transfer request")
	}
	for _, char := range transferID {
		if !(char >= 'a' && char <= 'z') && !(char >= 'A' && char <= 'Z') && !(char >= '0' && char <= '9') && char != '-' && char != '_' {
			return "", fmt.Errorf("invalid file transfer id")
		}
	}
	base := path.Base(targetPath)
	if base == "." || base == "/" || base == "" {
		return "", fmt.Errorf("invalid file transfer target")
	}
	return path.Join(path.Dir(targetPath), fmt.Sprintf(".%s.jms-transfer-%s.part", base, transferID)), nil
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func isTransferStageMissing(err error) bool {
	if os.IsNotExist(err) {
		return true
	}
	var statusErr *sftp.StatusError
	return errors.As(err, &statusErr) && statusErr.FxCode() == sftp.ErrSSHFxNoSuchFile
}

func (u *UserWebVolume) prepareTransfer(transferID, targetPath string, totalSize int64, conflictPolicy string) (webSftpTransferResponse, error) {
	if totalSize < 0 {
		return webSftpTransferResponse{}, fmt.Errorf("invalid file transfer size")
	}
	stagePath, err := transferStagePath(targetPath, transferID)
	if err != nil {
		return webSftpTransferResponse{}, err
	}
	if info, statErr := u.UserSftp.Stat(stagePath); statErr == nil {
		if info.Size() > totalSize {
			return webSftpTransferResponse{}, fmt.Errorf("file transfer stage exceeds expected size")
		}
		return webSftpTransferResponse{TransferID: transferID, CommittedBytes: info.Size(), TotalBytes: totalSize, State: "ready"}, nil
	}

	if _, statErr := u.UserSftp.Stat(targetPath); statErr == nil {
		if conflictPolicy == "skip" {
			return webSftpTransferResponse{TransferID: transferID, TotalBytes: totalSize, State: "skipped"}, nil
		}
		if conflictPolicy == "ask" {
			return webSftpTransferResponse{TransferID: transferID, TotalBytes: totalSize, State: "conflict"}, nil
		}
		if conflictPolicy != "overwrite" {
			return webSftpTransferResponse{}, fmt.Errorf("target file already exists")
		}
	}

	file, err := u.UserSftp.Create(stagePath)
	if err != nil {
		return webSftpTransferResponse{}, err
	}
	if err = file.Close(); err != nil {
		return webSftpTransferResponse{}, err
	}
	return webSftpTransferResponse{TransferID: transferID, TotalBytes: totalSize, State: "ready"}, nil
}

func (u *UserWebVolume) readTransferChunk(sourcePath, transferID string, offset, length int64) ([]byte, webSftpTransferChunk, error) {
	if offset < 0 || length <= 0 || length > transferChunkMaxSize {
		return nil, webSftpTransferChunk{}, fmt.Errorf("invalid file transfer range")
	}
	file, err := u.UserSftp.Open(sourcePath)
	if err != nil {
		return nil, webSftpTransferChunk{}, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, webSftpTransferChunk{}, err
	}
	if offset >= info.Size() {
		return nil, webSftpTransferChunk{Offset: offset, EOF: true}, nil
	}
	remaining := info.Size() - offset
	if length > remaining {
		length = remaining
	}
	data := make([]byte, length)
	n, readErr := file.ReadAt(data, offset)
	if readErr != nil && readErr != io.EOF {
		return nil, webSftpTransferChunk{}, readErr
	}
	data = data[:n]
	return data, webSftpTransferChunk{
		Offset: offset,
		SHA256: sha256Hex(data),
		EOF:    offset+int64(n) == info.Size(),
	}, nil
}

func (u *UserWebVolume) writeTransferChunk(transferID, targetPath string, totalSize, offset int64, expectedSHA256 string, data []byte) (webSftpTransferResponse, error) {
	if totalSize < 0 || offset < 0 || len(data) == 0 || offset+int64(len(data)) > totalSize || expectedSHA256 != sha256Hex(data) {
		return webSftpTransferResponse{}, fmt.Errorf("invalid file transfer chunk")
	}
	stagePath, err := transferStagePath(targetPath, transferID)
	if err != nil {
		return webSftpTransferResponse{}, err
	}
	file, err := u.UserSftp.OpenForWrite(stagePath)
	// Some SFTP servers do not retain a just-created zero-byte file between
	// separate requests. The first chunk can safely recreate it; subsequent
	// chunks must keep failing so a resumable transfer never skips data.
	created := false
	if err != nil && offset == 0 && isTransferStageMissing(err) {
		file, err = u.UserSftp.Create(stagePath)
		created = err == nil
	}
	if err != nil {
		return webSftpTransferResponse{}, err
	}
	defer file.Close()
	committedBytes := int64(0)
	if !created {
		info, statErr := u.UserSftp.Stat(stagePath)
		if statErr != nil {
			return webSftpTransferResponse{}, statErr
		}
		committedBytes = info.Size()
	}
	if committedBytes > totalSize || offset > committedBytes {
		return webSftpTransferResponse{}, fmt.Errorf("file transfer chunk offset is out of order")
	}
	if offset < committedBytes {
		existing := make([]byte, len(data))
		n, readErr := file.ReadAt(existing, offset)
		if readErr != nil && readErr != io.EOF {
			return webSftpTransferResponse{}, readErr
		}
		if n != len(data) || !strings.EqualFold(sha256Hex(existing), expectedSHA256) {
			return webSftpTransferResponse{}, fmt.Errorf("file transfer duplicate chunk does not match")
		}
		return webSftpTransferResponse{TransferID: transferID, CommittedBytes: committedBytes, TotalBytes: totalSize, State: "ready", Duplicate: true}, nil
	}
	if _, err = file.WriteAt(data, offset); err != nil {
		return webSftpTransferResponse{}, err
	}
	return webSftpTransferResponse{TransferID: transferID, CommittedBytes: offset + int64(len(data)), TotalBytes: totalSize, State: "ready"}, nil
}

func (u *UserWebVolume) transferStatus(transferID, targetPath string, totalSize int64) (webSftpTransferResponse, error) {
	stagePath, err := transferStagePath(targetPath, transferID)
	if err != nil {
		return webSftpTransferResponse{}, err
	}
	info, err := u.UserSftp.Stat(stagePath)
	if err != nil {
		return webSftpTransferResponse{TransferID: transferID, TotalBytes: totalSize, State: "missing"}, nil
	}
	if info.Size() > totalSize {
		return webSftpTransferResponse{}, fmt.Errorf("file transfer stage exceeds expected size")
	}
	return webSftpTransferResponse{TransferID: transferID, CommittedBytes: info.Size(), TotalBytes: totalSize, State: "ready"}, nil
}

func (u *UserWebVolume) commitTransfer(transferID, targetPath string, totalSize int64, expectedSHA256, conflictPolicy string) (webSftpTransferResponse, error) {
	stagePath, err := transferStagePath(targetPath, transferID)
	if err != nil {
		return webSftpTransferResponse{}, err
	}
	file, err := u.UserSftp.Open(stagePath)
	if err != nil {
		return webSftpTransferResponse{}, err
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return webSftpTransferResponse{}, err
	}
	if info.Size() != totalSize {
		_ = file.Close()
		return webSftpTransferResponse{}, fmt.Errorf("file transfer is incomplete")
	}
	hash := sha256.New()
	if _, err = io.Copy(hash, file); err != nil {
		_ = file.Close()
		return webSftpTransferResponse{}, err
	}
	if err = file.Close(); err != nil {
		return webSftpTransferResponse{}, err
	}
	if !strings.EqualFold(hex.EncodeToString(hash.Sum(nil)), expectedSHA256) {
		return webSftpTransferResponse{}, fmt.Errorf("file transfer checksum mismatch")
	}
	if conflictPolicy == "overwrite" {
		err = u.UserSftp.PosixRename(stagePath, targetPath)
	} else {
		err = u.UserSftp.Rename(stagePath, targetPath)
	}
	if err != nil {
		return webSftpTransferResponse{}, err
	}
	return webSftpTransferResponse{TransferID: transferID, CommittedBytes: totalSize, TotalBytes: totalSize, State: "completed"}, nil
}

func (u *UserWebVolume) cancelTransfer(transferID, targetPath string, discard bool) (webSftpTransferResponse, error) {
	stagePath, err := transferStagePath(targetPath, transferID)
	if err != nil {
		return webSftpTransferResponse{}, err
	}
	if discard {
		if err = u.UserSftp.Remove(stagePath); err != nil && !isTransferStageMissing(err) {
			return webSftpTransferResponse{}, err
		}
	}
	return webSftpTransferResponse{TransferID: transferID, State: "ready"}, nil
}

func (h *webSftp) handleTransferRead(request *webSftpRequest, response *Message) {
	data, metadata, err := h.volume.readTransferChunk(request.Path, request.TransferID, request.OffSet, request.Length)
	if err != nil {
		response.Err = err.Error()
		h.ws.SendMessage(response)
		return
	}
	payload, err := json.Marshal(metadata)
	if err != nil {
		response.Err = err.Error()
		h.ws.SendMessage(response)
		return
	}
	response.Type = SFTPBinary
	response.Data = string(payload)
	response.Raw = data
	h.ws.SendMessage(response)
}

func (h *webSftp) handleTransferMutation(request *webSftpRequest, msg *Message, response *Message) {
	var (
		result webSftpTransferResponse
		err    error
	)
	switch msg.Cmd {
	case "transfer_prepare":
		result, err = h.volume.prepareTransfer(request.TransferID, request.Path, request.Size, request.ConflictPolicy)
	case "transfer_write":
		result, err = h.volume.writeTransferChunk(request.TransferID, request.Path, request.Size, request.OffSet, request.SHA256, msg.Raw)
	case "transfer_status":
		result, err = h.volume.transferStatus(request.TransferID, request.Path, request.Size)
	case "transfer_commit":
		result, err = h.volume.commitTransfer(request.TransferID, request.Path, request.Size, request.SHA256, request.ConflictPolicy)
	case "transfer_cancel":
		result, err = h.volume.cancelTransfer(request.TransferID, request.Path, request.Discard)
	}
	if err != nil {
		response.Err = err.Error()
		h.ws.SendMessage(response)
		return
	}
	payload, err := json.Marshal(result)
	if err != nil {
		response.Err = err.Error()
	} else {
		response.Data = string(payload)
	}
	h.ws.SendMessage(response)
}
