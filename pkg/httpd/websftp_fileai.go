package httpd

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/jumpserver/koko/pkg/common"
	"github.com/jumpserver/koko/pkg/fileai"
	"github.com/jumpserver/koko/pkg/logger"
	"github.com/jumpserver/koko/pkg/terminalai"
)

const maxFileAIMessageBytes = 256 * 1024

type webSFTPFileExecutor struct {
	volume       *UserWebVolume
	guard        func() error
	validatePath func(string) error
	canDownload  func() bool
	canUpload    func() bool
	canDelete    func() bool
}

func (h *webSftp) initializeFileAI(settings terminalai.Settings) {
	fileSession, err := fileai.NewSession(fileai.SessionOptions{
		Config: terminalai.NewConfigFromSettings(settings),
		Executor: &webSFTPFileExecutor{
			volume:       h.volume,
			validatePath: h.volume.UserSftp.ValidateFileAIPath,
			guard: func() error {
				if h.sessionExpired() {
					return fmt.Errorf("session expired or not found")
				}
				return nil
			},
			canDownload: func() bool {
				return h.ws.ConnectToken == nil ||
					h.ws.ConnectToken.Actions.EnableDownload()
			},
			canUpload: func() bool {
				return h.ws.ConnectToken == nil ||
					h.ws.ConnectToken.Actions.EnableUpload()
			},
			canDelete: func() bool {
				return h.ws.ConnectToken == nil ||
					h.ws.ConnectToken.Actions.EnableDelete()
			},
		},
		Language: h.ws.langCode,
		Emit: func(message fileai.ChatMessage) {
			data, marshalErr := json.Marshal(message)
			if marshalErr != nil {
				return
			}
			h.ws.SendMessage(&Message{
				Id: h.ws.Uuid, Type: ChatMessage, Data: string(data),
			})
		},
	})
	if err != nil {
		logger.Infof("File AI unavailable for SFTP websocket %s: %s", h.ws.Uuid, err)
		return
	}
	h.setFileAI(fileSession)
	info := fileSession.ProviderInfo()
	logger.Infof(
		"File AI provider %s model %s initialized for SFTP websocket %s",
		info.Name, info.Model, h.ws.Uuid,
	)
}

func (h *webSftp) handleFileAIMessage(msg *Message) {
	if len(msg.Data) > maxFileAIMessageBytes {
		h.sendFileAIError("", fmt.Errorf("file AI message is too large"), false)
		return
	}
	chatMessage, err := terminalai.DecodeChatMessage(msg.Data)
	if err != nil {
		h.sendFileAIError("", err, false)
		return
	}
	targetID, _ := chatMessage.Metadata["targetId"].(string)
	if h.trackedSessionExpired() {
		h.sendFileAIError(targetID, fmt.Errorf("session expired or not found"), false)
		h.ws.SendMessage(&Message{Id: h.ws.Uuid, Type: CLOSE})
		return
	}
	fileSession := h.getFileAI()
	if fileSession == nil {
		h.sendFileAIError(targetID, fmt.Errorf("file AI is unavailable"), true)
		return
	}
	if err := fileSession.Handle(chatMessage); err != nil {
		h.sendFileAIError(targetID, err, false)
	}
}

func (h *webSftp) sendFileAIError(targetID string, value error, disabled bool) {
	metadata := map[string]any{
		"domain": "file", "targetId": targetID, "stage": "final",
	}
	parts := make([]terminalai.ChatPart, 0, 2)
	if disabled {
		parts = append(parts, terminalai.ChatPart{
			Type: "data-capability",
			Data: map[string]any{"enabled": false, "tools": []string{}},
		})
	}
	parts = append(parts, terminalai.ChatPart{
		Type: "data-error", Data: map[string]any{"message": value.Error()},
	})
	message := terminalai.ChatMessage{
		ID: common.UUID(), Role: "assistant", Metadata: metadata, Parts: parts,
	}
	data, err := json.Marshal(message)
	if err != nil {
		return
	}
	h.ws.SendMessage(&Message{
		Id: h.ws.Uuid, Type: ChatMessage, Data: string(data),
	})
}

func (e *webSFTPFileExecutor) ListDirectory(
	ctx context.Context,
	path string,
	limit int,
) (fileai.DirectoryResult, error) {
	var result fileai.DirectoryResult
	if err := e.checkPaths(ctx, path); err != nil {
		return result, err
	}
	if limit <= 0 || limit > fileai.MaxDirectoryEntries {
		limit = fileai.MaxDirectoryEntries
	}
	entries, err := e.volume.UserSftp.ReadDir(path)
	if err != nil {
		return result, err
	}
	result.Path = path
	result.Truncated = len(entries) > limit
	if len(entries) > limit {
		entries = entries[:limit]
	}
	result.Entries = make([]fileai.Entry, 0, len(entries))
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return fileai.DirectoryResult{}, err
		}
		result.Entries = append(
			result.Entries,
			fileAIEntry(filepath.Join(path, entry.Name()), entry),
		)
	}
	return result, nil
}

func (e *webSFTPFileExecutor) Stat(
	ctx context.Context,
	path string,
) (fileai.Entry, error) {
	if err := e.checkPaths(ctx, path); err != nil {
		return fileai.Entry{}, err
	}
	entry, err := e.volume.UserSftp.Stat(path)
	if err != nil {
		return fileai.Entry{}, err
	}
	return fileAIEntry(path, entry), nil
}

func (e *webSFTPFileExecutor) ReadText(
	ctx context.Context,
	path string,
	limit int64,
) (fileai.TextResult, error) {
	var result fileai.TextResult
	if err := requireFilePermission(e.canDownload); err != nil {
		return result, err
	}
	entry, err := e.Stat(ctx, path)
	if err != nil {
		return result, err
	}
	if entry.IsDir {
		return result, fmt.Errorf("cannot read a directory as text")
	}
	if limit <= 0 || limit > fileai.MaxTextBytes {
		limit = fileai.MaxTextBytes
	}
	if entry.Size > limit {
		return result, fmt.Errorf("file exceeds the file AI text limit")
	}
	file, _, err := e.volume.Download(path, false)
	if err != nil {
		return result, err
	}
	if file.Reader == nil {
		return result, fmt.Errorf("remote file has no readable content")
	}
	defer file.Reader.Close()
	data, err := io.ReadAll(io.LimitReader(file.Reader, limit+1))
	if err != nil {
		return result, err
	}
	truncated := int64(len(data)) > limit
	if truncated {
		data = data[:limit]
		for len(data) > 0 && !utf8.Valid(data) {
			data = data[:len(data)-1]
		}
	}
	if !utf8.Valid(data) || bytes.IndexByte(data, 0) >= 0 {
		return result, fmt.Errorf("file AI can read UTF-8 text files only")
	}
	version := entry.Version
	if !truncated {
		digest := sha256.Sum256(data)
		version = "sha256:" + hex.EncodeToString(digest[:])
	}
	return fileai.TextResult{
		Path: path, Exists: true, Content: string(data), Version: version,
		Truncated: truncated,
	}, nil
}

func (e *webSFTPFileExecutor) SaveText(
	ctx context.Context,
	path, content, expectedVersion string,
) (fileai.Entry, error) {
	if err := requireFilePermission(e.canUpload); err != nil {
		return fileai.Entry{}, err
	}
	if err := e.checkPaths(ctx, path); err != nil {
		return fileai.Entry{}, err
	}
	if len(content) > fileai.MaxTextBytes || !utf8.ValidString(content) ||
		strings.IndexByte(content, 0) >= 0 {
		return fileai.Entry{}, fmt.Errorf("invalid file AI text content")
	}
	volumeExpectedVersion := expectedVersion
	if expectedVersion == fileai.ExpectedVersionAbsent {
		volumeExpectedVersion = webSftpAbsentVersion
	}
	entry, err := e.volume.SaveFile(
		path,
		bytes.NewReader([]byte(content)),
		int64(len(content)),
		&volumeExpectedVersion,
		false,
	)
	if err != nil {
		return fileai.Entry{}, err
	}
	size := int64(len(content))
	if parsed, parseErr := parseFileInfoSize(entry.Size); parseErr == nil {
		size = parsed
	}
	return fileai.Entry{
		Name: entry.Name, Path: path, Exists: true, Size: size, Perm: entry.Perm,
		ModTime: parseFileInfoModTime(entry.ModTime), IsDir: entry.IsDir,
		Version: entry.Version,
	}, nil
}

func (e *webSFTPFileExecutor) Mkdir(ctx context.Context, path string) error {
	if err := requireFilePermission(e.canUpload); err != nil {
		return err
	}
	if err := e.checkPaths(ctx, path); err != nil {
		return err
	}
	return e.volume.MakeDir(path)
}

func (e *webSFTPFileExecutor) Rename(
	ctx context.Context,
	path, destinationPath string,
) error {
	if err := requireFilePermission(e.canUpload); err != nil {
		return err
	}
	if err := e.checkPaths(ctx, path, destinationPath); err != nil {
		return err
	}
	if filepath.Clean(filepath.Dir(path)) != filepath.Clean(filepath.Dir(destinationPath)) {
		return fmt.Errorf("rename destination must remain in the same directory")
	}
	return e.volume.Rename(path, filepath.Base(destinationPath))
}

func (e *webSFTPFileExecutor) Delete(ctx context.Context, path string) error {
	if err := requireFilePermission(e.canDelete); err != nil {
		return err
	}
	if err := e.checkPaths(ctx, path); err != nil {
		return err
	}
	return e.volume.Remove(path)
}

func requireFilePermission(check func() bool) error {
	if check != nil && !check() {
		return fmt.Errorf("permission denied")
	}
	return nil
}

func (e *webSFTPFileExecutor) check(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if e.guard != nil {
		return e.guard()
	}
	return nil
}

func (e *webSFTPFileExecutor) checkPaths(ctx context.Context, paths ...string) error {
	if err := e.check(ctx); err != nil {
		return err
	}
	validate := e.validatePath
	if validate == nil && e.volume != nil && e.volume.UserSftp != nil {
		validate = e.volume.UserSftp.ValidateFileAIPath
	}
	if validate == nil {
		return fmt.Errorf("file AI path validator is unavailable")
	}
	for _, path := range paths {
		if err := validate(path); err != nil {
			return err
		}
	}
	return nil
}

func fileAIEntry(path string, info os.FileInfo) fileai.Entry {
	return fileai.Entry{
		Name: info.Name(), Path: path, Exists: true, Size: info.Size(),
		Perm: info.Mode().String(), ModTime: info.ModTime().Unix(),
		IsDir: info.IsDir(), Version: webSftpFileVersion(info),
	}
}

func parseFileInfoSize(value string) (int64, error) {
	var size int64
	_, err := fmt.Sscan(value, &size)
	return size, err
}

func parseFileInfoModTime(value string) int64 {
	var timestamp int64
	_, _ = fmt.Sscan(value, &timestamp)
	return timestamp
}
