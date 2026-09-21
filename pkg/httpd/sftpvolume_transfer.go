package httpd

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"strings"
	"sync"

	"github.com/pkg/sftp"

	"github.com/jumpserver-dev/sdk-go/model"
	"github.com/jumpserver/koko/pkg/logger"
)

const transferChunkMaxSize = 2 * 1024 * 1024

// transferKeepBothCommitMu 保证当前进程内所有 WebSocket 会话的后缀选取与最终重命名连续执行。
var transferKeepBothCommitMu sync.Mutex

type sftpTransferResult struct {
	TransferID     string `json:"transfer_id"`
	CommittedBytes int64  `json:"committed_bytes"`
	TotalBytes     int64  `json:"total_bytes"`
	State          string `json:"state"`
	Duplicate      bool   `json:"duplicate,omitempty"`
}

type sftpTransferChunk struct {
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

func (u *sftpVolume) openTransferRead(path string) (transferIO, error) {
	if u.openRead != nil {
		return u.openRead(path)
	}
	return u.conn.Open(path)
}

func (u *sftpVolume) openTransferWrite(path string, create bool) (transferIO, error) {
	if u.openWrite != nil {
		return u.openWrite(path, create)
	}
	return u.conn.OpenUploadTemp(path, create)
}

func (u *sftpVolume) closeTransferReadLocked() {
	if u.transferRead == nil {
		return
	}
	_ = u.transferRead.file.Close()
	u.transferRead = nil
}

func (u *sftpVolume) closeTransferWriteLocked() {
	if u.transferWrite == nil {
		return
	}
	_ = u.transferWrite.file.Close()
	u.transferWrite = nil
}

func (u *sftpVolume) cacheWriteLocked(transferID, path string, file transferIO, committed int64) {
	u.closeTransferWriteLocked()
	if transferID == "" {
		return
	}
	u.transferWrite = &cachedTransferFile{id: transferID, path: path, file: file, committed: committed}
}

func isSftpNotExist(err error) bool {
	if errors.Is(err, os.ErrNotExist) || errors.Is(err, sftp.ErrSSHFxNoSuchFile) {
		return true
	}
	var statusErr *sftp.StatusError
	return errors.As(err, &statusErr) && statusErr.FxCode() == sftp.ErrSSHFxNoSuchFile
}

// nextTransferTargetPath 在目标路径可用时直接返回原路径，否则按照“名称 (序号).扩展名”的规则
// 查找第一个可用的同级路径。没有独立扩展名的点文件会保留完整名称，例如“.env”会变为“.env (1)”。
func nextTransferTargetPath(targetPath string, exists func(string) (bool, error)) (string, error) {
	available, err := exists(targetPath)
	if err != nil || !available {
		return targetPath, err
	}
	filename := path.Base(targetPath)
	extension := path.Ext(filename)
	if strings.TrimSuffix(filename, extension) == "" {
		extension = ""
	}
	base := strings.TrimSuffix(filename, extension)
	directory := path.Dir(targetPath)
	for index := 1; index <= 10000; index++ {
		candidate := path.Join(directory, fmt.Sprintf("%s (%d)%s", base, index, extension))
		occupied, statErr := exists(candidate)
		if statErr != nil {
			return "", statErr
		}
		if !occupied {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("unable to find an available file transfer target")
}

// transferTargetExists 区分 SFTP 路径不存在与其他 Stat 错误，避免把权限或连接错误误判为路径可用。
func (u *sftpVolume) transferTargetExists(targetPath string) (bool, error) {
	_, err := u.conn.Stat(targetPath)
	if err == nil {
		return true, nil
	}
	if isSftpNotExist(err) {
		return false, nil
	}
	return false, err
}

// commitKeepBothTarget 在当前进程内串行执行候选路径选择和重命名。如果其他进程在 Stat 与 Rename
// 之间抢占候选路径，则识别该冲突并使用下一个可用后缀重试。
func commitKeepBothTarget(
	targetPath string,
	exists func(string) (bool, error),
	rename func(string) error,
) (string, error) {
	transferKeepBothCommitMu.Lock()
	defer transferKeepBothCommitMu.Unlock()

	for attempts := 0; attempts < 10000; attempts++ {
		candidate, err := nextTransferTargetPath(targetPath, exists)
		if err != nil {
			return "", err
		}
		if err = rename(candidate); err == nil {
			return candidate, nil
		}
		occupied, statErr := exists(candidate)
		if statErr != nil {
			return "", statErr
		}
		if !occupied {
			return "", err
		}
	}
	return "", fmt.Errorf("unable to commit an available file transfer target")
}

func (u *sftpVolume) prepareTransfer(transferID, targetPath string, totalSize int64, conflictPolicy string) (sftpTransferResult, error) {
	u.lock.Lock()
	defer u.lock.Unlock()
	if u.closed.Load() {
		return sftpTransferResult{}, os.ErrClosed
	}
	if totalSize < 0 {
		return sftpTransferResult{}, fmt.Errorf("invalid file transfer size")
	}
	if conflictPolicy != "ask" && conflictPolicy != "overwrite" && conflictPolicy != "skip" && conflictPolicy != "keep_both" {
		return sftpTransferResult{}, fmt.Errorf("invalid file transfer conflict policy")
	}
	stagePath, err := transferStagePath(targetPath, transferID)
	if err != nil {
		return sftpTransferResult{}, err
	}
	if info, statErr := u.conn.Stat(stagePath); statErr == nil {
		if info.Size() > totalSize {
			return sftpTransferResult{}, fmt.Errorf("file transfer stage exceeds expected size")
		}
		file, openErr := u.openTransferWrite(stagePath, false)
		if openErr != nil {
			return sftpTransferResult{}, openErr
		}
		u.cacheWriteLocked(transferID, stagePath, file, info.Size())
		return sftpTransferResult{TransferID: transferID, CommittedBytes: info.Size(), TotalBytes: totalSize, State: "ready"}, nil
	} else if !isSftpNotExist(statErr) {
		return sftpTransferResult{}, statErr
	}

	if _, statErr := u.conn.Stat(targetPath); statErr == nil {
		if conflictPolicy == "skip" {
			return sftpTransferResult{TransferID: transferID, TotalBytes: totalSize, State: "skipped"}, nil
		}
		if conflictPolicy == "ask" {
			return sftpTransferResult{TransferID: transferID, TotalBytes: totalSize, State: "conflict"}, nil
		}
		if conflictPolicy != "overwrite" && conflictPolicy != "keep_both" {
			return sftpTransferResult{}, fmt.Errorf("target file already exists")
		}
	} else if !isSftpNotExist(statErr) {
		return sftpTransferResult{}, statErr
	}

	file, err := u.openTransferWrite(stagePath, true)
	if err != nil {
		return sftpTransferResult{}, err
	}
	u.cacheWriteLocked(transferID, stagePath, file, 0)
	return sftpTransferResult{TransferID: transferID, TotalBytes: totalSize, State: "ready"}, nil
}

func (u *sftpVolume) readTransferChunk(transferID, sourcePath string, offset, length int64) ([]byte, sftpTransferChunk, error) {
	u.lock.Lock()
	defer u.lock.Unlock()
	if u.closed.Load() {
		return nil, sftpTransferChunk{}, os.ErrClosed
	}
	if offset < 0 || length <= 0 || length > transferChunkMaxSize {
		return nil, sftpTransferChunk{}, fmt.Errorf("invalid file transfer range")
	}
	file, cached, err := u.readHandleLocked(transferID, sourcePath)
	if err != nil {
		return nil, sftpTransferChunk{}, err
	}
	if !cached {
		defer file.Close()
	}
	info, err := file.Stat()
	if err != nil {
		u.closeTransferReadLocked()
		return nil, sftpTransferChunk{}, err
	}
	if offset >= info.Size() {
		u.closeTransferReadLocked()
		return nil, sftpTransferChunk{Offset: offset, SHA256: sha256Hex(nil), EOF: true}, nil
	}
	remaining := info.Size() - offset
	if length > remaining {
		length = remaining
	}
	data := make([]byte, length)
	n, readErr := file.ReadAt(data, offset)
	if readErr != nil && readErr != io.EOF {
		u.closeTransferReadLocked()
		return nil, sftpTransferChunk{}, readErr
	}
	data = data[:n]
	eof := offset+int64(n) == info.Size()
	if eof {
		u.closeTransferReadLocked()
	}
	return data, sftpTransferChunk{
		Offset: offset,
		SHA256: sha256Hex(data),
		EOF:    eof,
	}, nil
}

func (u *sftpVolume) readHandleLocked(transferID, path string) (file transferIO, cached bool, err error) {
	if transferID != "" && u.transferRead != nil && u.transferRead.id == transferID && u.transferRead.path == path {
		return u.transferRead.file, true, nil
	}
	u.closeTransferReadLocked()
	file, err = u.openTransferRead(path)
	if err != nil {
		return nil, false, err
	}
	if transferID == "" {
		return file, false, nil
	}
	u.transferRead = &cachedTransferFile{id: transferID, path: path, file: file}
	return file, true, nil
}

func (u *sftpVolume) writeTransferChunk(transferID, targetPath string, totalSize, offset int64, expectedSHA256 string, data []byte) (result sftpTransferResult, err error) {
	u.lock.Lock()
	defer u.lock.Unlock()
	if u.closed.Load() {
		return sftpTransferResult{}, os.ErrClosed
	}
	if totalSize < 0 || offset < 0 || offset > totalSize || len(data) == 0 || len(data) > transferChunkMaxSize || int64(len(data)) > totalSize-offset || !strings.EqualFold(expectedSHA256, sha256Hex(data)) {
		return sftpTransferResult{}, fmt.Errorf("invalid file transfer chunk")
	}
	stagePath, err := transferStagePath(targetPath, transferID)
	if err != nil {
		return sftpTransferResult{}, err
	}
	file, committedBytes, cached, err := u.writeHandleLocked(transferID, stagePath, offset)
	if err != nil {
		return sftpTransferResult{}, err
	}
	if !cached {
		defer func() {
			if closeErr := file.Close(); err == nil && closeErr != nil {
				result, err = sftpTransferResult{}, closeErr
			}
		}()
	}
	if committedBytes > totalSize || offset > committedBytes {
		return sftpTransferResult{}, fmt.Errorf("file transfer chunk offset is out of order")
	}
	if offset < committedBytes {
		existing := make([]byte, len(data))
		n, readErr := file.ReadAt(existing, offset)
		if readErr != nil && readErr != io.EOF {
			return sftpTransferResult{}, readErr
		}
		if n != len(data) || !strings.EqualFold(sha256Hex(existing), expectedSHA256) {
			return sftpTransferResult{}, fmt.Errorf("file transfer duplicate chunk does not match")
		}
		return sftpTransferResult{TransferID: transferID, CommittedBytes: committedBytes, TotalBytes: totalSize, State: "ready", Duplicate: true}, nil
	}
	if n, writeErr := file.WriteAt(data, offset); writeErr != nil {
		if cached {
			u.closeTransferWriteLocked()
		}
		return sftpTransferResult{}, writeErr
	} else if n != len(data) {
		if cached {
			u.closeTransferWriteLocked()
		}
		return sftpTransferResult{}, io.ErrShortWrite
	}
	committedBytes = offset + int64(len(data))
	if cached {
		u.transferWrite.committed = committedBytes
	}
	return sftpTransferResult{TransferID: transferID, CommittedBytes: committedBytes, TotalBytes: totalSize, State: "ready"}, nil
}

func (u *sftpVolume) writeHandleLocked(transferID, path string, offset int64) (file transferIO, committed int64, cached bool, err error) {
	if transferID != "" && u.transferWrite != nil && u.transferWrite.id == transferID && u.transferWrite.path == path {
		return u.transferWrite.file, u.transferWrite.committed, true, nil
	}
	u.closeTransferWriteLocked()
	file, err = u.openTransferWrite(path, false)
	created := false
	// Some SFTP servers do not retain a just-created zero-byte file between
	// separate requests. The first chunk can safely recreate it; subsequent
	// chunks must keep failing so a resumable transfer never skips data.
	if err != nil && offset == 0 && isSftpNotExist(err) {
		file, err = u.openTransferWrite(path, true)
		created = err == nil
	}
	if err != nil {
		return nil, 0, false, err
	}
	if !created {
		info, statErr := file.Stat()
		if statErr != nil {
			_ = file.Close()
			return nil, 0, false, statErr
		}
		committed = info.Size()
	}
	if transferID == "" {
		return file, committed, false, nil
	}
	u.transferWrite = &cachedTransferFile{id: transferID, path: path, file: file, committed: committed}
	return file, committed, true, nil
}

func (u *sftpVolume) transferStatus(transferID, targetPath string, totalSize int64) (sftpTransferResult, error) {
	u.lock.Lock()
	defer u.lock.Unlock()
	if u.closed.Load() {
		return sftpTransferResult{}, os.ErrClosed
	}
	if totalSize < 0 {
		return sftpTransferResult{}, fmt.Errorf("invalid file transfer size")
	}
	stagePath, err := transferStagePath(targetPath, transferID)
	if err != nil {
		return sftpTransferResult{}, err
	}
	info, err := u.conn.Stat(stagePath)
	if err != nil {
		if isSftpNotExist(err) {
			return sftpTransferResult{TransferID: transferID, TotalBytes: totalSize, State: "missing"}, nil
		}
		return sftpTransferResult{}, err
	}
	if info.Size() > totalSize {
		return sftpTransferResult{}, fmt.Errorf("file transfer stage exceeds expected size")
	}
	return sftpTransferResult{TransferID: transferID, CommittedBytes: info.Size(), TotalBytes: totalSize, State: "ready"}, nil
}

func (u *sftpVolume) commitTransfer(transferID, targetPath string, totalSize int64, expectedSHA256, conflictPolicy string) (sftpTransferResult, error) {
	u.lock.Lock()
	defer u.lock.Unlock()
	if u.closed.Load() {
		return sftpTransferResult{}, os.ErrClosed
	}
	if totalSize < 0 || (conflictPolicy != "ask" && conflictPolicy != "overwrite" && conflictPolicy != "skip" && conflictPolicy != "keep_both") {
		return sftpTransferResult{}, fmt.Errorf("invalid file transfer commit")
	}
	stagePath, err := transferStagePath(targetPath, transferID)
	if err != nil {
		return sftpTransferResult{}, err
	}
	if u.transferWrite != nil && u.transferWrite.id == transferID && u.transferWrite.path == stagePath {
		u.closeTransferWriteLocked()
	}
	file, err := u.conn.OpenUploadTemp(stagePath, false)
	if err != nil {
		return sftpTransferResult{}, err
	}
	closed := false
	defer func() {
		if !closed {
			_ = file.Close()
		}
	}()
	info, err := file.Stat()
	if err != nil {
		return sftpTransferResult{}, err
	}
	if info.Size() != totalSize {
		return sftpTransferResult{}, fmt.Errorf("file transfer is incomplete")
	}
	hash := sha256.New()
	if _, err = io.CopyN(hash, file, totalSize); err != nil {
		return sftpTransferResult{}, err
	}
	if info, err = file.Stat(); err != nil {
		return sftpTransferResult{}, err
	} else if info.Size() != totalSize {
		return sftpTransferResult{}, fmt.Errorf("file transfer size changed during commit")
	}
	if !strings.EqualFold(hex.EncodeToString(hash.Sum(nil)), expectedSHA256) {
		return sftpTransferResult{}, fmt.Errorf("file transfer checksum mismatch")
	}
	// Some servers cannot rename an open file. Release the verification handle
	// before committing, and never turn a completed upload into a close failure.
	closed = true
	if err := file.Close(); err != nil {
		return sftpTransferResult{}, err
	}
	var ftpLog *model.FTPLog
	if conflictPolicy == "keep_both" {
		targetPath, err = commitKeepBothTarget(targetPath, u.transferTargetExists, func(candidate string) error {
			var commitErr error
			ftpLog, commitErr = u.conn.CommitUploadTemp(stagePath, candidate, false)
			return commitErr
		})
		if err != nil {
			return sftpTransferResult{}, err
		}
	} else {
		ftpLog, err = u.conn.CommitUploadTemp(stagePath, targetPath, conflictPolicy == "overwrite")
	}
	if err != nil {
		return sftpTransferResult{}, err
	}
	u.recordUploadedFile(targetPath, totalSize, expectedSHA256, ftpLog)
	return sftpTransferResult{TransferID: transferID, CommittedBytes: totalSize, TotalBytes: totalSize, State: "completed"}, nil
}

func (u *sftpVolume) recordUploadedFile(path string, size int64, expectedSHA256 string, ftpLog *model.FTPLog) {
	if u.recorder == nil || ftpLog == nil || size >= u.recorder.MaxFileSize {
		return
	}
	file, err := u.conn.OpenUploadTemp(path, false)
	if err != nil {
		logger.Errorf("Open completed upload for recording: %s", err)
		return
	}
	if err := u.recordUploadContents(ftpLog, file, size, expectedSHA256); err != nil {
		logger.Errorf("Record completed upload: %s", err)
	}
	if err := file.Close(); err != nil {
		logger.Errorf("Close completed upload recording: %s", err)
	}
}

// Hash exactly the bytes saved to the recording. A target replaced or modified
// after commit must never associate different contents with this upload's log.
func (u *sftpVolume) recordUploadContents(ftpLog *model.FTPLog, reader io.Reader, size int64, expectedSHA256 string) (err error) {
	if u.recorder == nil || ftpLog == nil || size >= u.recorder.MaxFileSize {
		return nil
	}
	defer func() {
		if err != nil {
			u.recorder.DiscardFTPFile(ftpLog.ID)
		}
	}()
	if size < 0 {
		return fmt.Errorf("invalid upload recording size")
	}
	buffer := make([]byte, 64*1024)
	hash := sha256.New()
	for offset := int64(0); ; {
		length := min(int64(len(buffer)), size-offset)
		chunk := buffer[:length]
		if _, err := io.ReadFull(reader, chunk); err != nil {
			return fmt.Errorf("read upload recording: %w", err)
		}
		_, _ = hash.Write(chunk)
		if err := u.recorder.ChunkedRecord(ftpLog, bytes.NewReader(chunk), offset, length); err != nil {
			return err
		}
		offset += length
		if offset == size {
			break
		}
	}
	var extra [1]byte
	if _, err := io.ReadFull(reader, extra[:]); err != io.EOF {
		if err != nil {
			return fmt.Errorf("read upload recording end: %w", err)
		}
		return fmt.Errorf("upload recording exceeds expected size")
	}
	if !strings.EqualFold(hex.EncodeToString(hash.Sum(nil)), expectedSHA256) {
		return fmt.Errorf("upload recording checksum mismatch")
	}
	u.recorder.FinishFTPFile(ftpLog.ID)
	return nil
}

func (u *sftpVolume) cancelTransfer(transferID, targetPath string, discard bool) (sftpTransferResult, error) {
	u.lock.Lock()
	defer u.lock.Unlock()
	if u.closed.Load() {
		return sftpTransferResult{}, os.ErrClosed
	}
	stagePath, err := transferStagePath(targetPath, transferID)
	if err != nil {
		return sftpTransferResult{}, err
	}
	if u.transferWrite != nil && u.transferWrite.id == transferID {
		u.closeTransferWriteLocked()
	}
	if discard {
		if err = u.conn.DiscardUploadTemp(stagePath); err != nil && !isSftpNotExist(err) {
			return sftpTransferResult{}, err
		}
	}
	return sftpTransferResult{TransferID: transferID, State: "ready"}, nil
}
