package httpd

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/jumpserver/koko/internal/sessiontools"
	"github.com/jumpserver/koko/pkg/logger"
	"github.com/jumpserver/koko/pkg/srvconn"
)

type webSFTPAgentToolExecutor struct {
	volume      *sftpVolume
	guard       func() error
	canDownload bool
	canUpload   bool
	canDelete   bool
}

func (h *webSftp) initializeFileTools() {
	if h.volume == nil || h.volume.conn == nil {
		logger.Errorf("SFTP websocket %s MCP file tools unavailable: SFTP resource is unavailable", h.ws.Uuid)
		return
	}
	if err := h.volume.conn.ValidateAgentToolConfinement(); err != nil {
		logger.Infof(
			"SFTP websocket %s MCP file tools disabled: %s",
			h.ws.Uuid, err,
		)
		return
	}
	resourceID := h.ws.Uuid
	contextSnapshot := sessiontools.ContextSnapshot{
		SessionKind: "file", InteractionMode: "live",
		CommandLanguage: "sftp", Protocol: srvconn.ProtocolSFTP,
	}
	if token := h.ws.ConnectToken; token != nil {
		resourceID = token.Id
		contextSnapshot = agentContextSnapshot(token, "file")
	}
	canDownload := h.ws.ConnectToken == nil || h.ws.ConnectToken.Actions.EnableDownload()
	canUpload := h.ws.ConnectToken == nil || h.ws.ConnectToken.Actions.EnableUpload()
	canDelete := h.ws.ConnectToken == nil || h.ws.ConnectToken.Actions.EnableDelete()
	executor := &webSFTPAgentToolExecutor{
		volume: h.volume,
		guard: func() error {
			if h.sessionExpired() {
				return fmt.Errorf("session expired or not found")
			}
			return nil
		},
		canDownload: canDownload,
		canUpload:   canUpload,
		canDelete:   canDelete,
	}
	handlers, err := sessiontools.NewFileToolHandlers(
		executor,
		sessiontools.FileToolCapabilities{
			ReadText: canDownload, SaveText: canUpload,
			Mkdir: canUpload, Rename: canUpload, Delete: canDelete,
		},
	)
	if err != nil {
		logger.Errorf("SFTP websocket %s MCP file tools unavailable: %s", h.ws.Uuid, err)
		return
	}
	dispatcher, err := sessiontools.NewMCPDispatcher(
		h.ws.ctx.Request.Context(),
		sessiontools.MCPDispatcherOptions{
			ResourceSessionID: resourceID, Profile: "file",
			Context: contextSnapshot, Handlers: handlers,
			Emit: func(outbound sessiontools.MCPOutbound) {
				h.ws.SendMessage(&Message{
					Id: h.ws.Uuid, Type: outbound.Type,
					Version:           sessiontools.MCPProtocolVersion,
					ResourceSessionID: resourceID, Data: string(outbound.Data),
				})
			},
		},
	)
	if err != nil {
		logger.Errorf("SFTP websocket %s MCP dispatcher unavailable: %s", h.ws.Uuid, err)
		return
	}
	h.resourceSessionID = resourceID
	h.setMCP(dispatcher)
	if err = dispatcher.AnnounceManifest(); err != nil {
		logger.Errorf("SFTP websocket %s MCP manifest failed: %s", h.ws.Uuid, err)
	}
}

func (h *webSftp) handleFileToolMessage(msg *Message) {
	if h.sessionExpired() {
		h.ws.SendMessage(&Message{Id: h.ws.Uuid, Type: CLOSE})
		return
	}
	dispatcher := h.getMCP()
	if dispatcher == nil {
		h.sendMCPError(msg, fmt.Errorf("file tools are unavailable"))
		return
	}
	if msg.Version != sessiontools.MCPProtocolVersion ||
		msg.ResourceSessionID != h.resourceSessionID {
		h.sendMCPError(msg, fmt.Errorf("MCP frame binding does not match"))
		return
	}
	var err error
	if msg.Type == MCPRequest {
		err = dispatcher.HandleRequest([]byte(msg.Data))
	} else {
		err = dispatcher.HandleCancel([]byte(msg.Data))
	}
	if err != nil {
		h.sendMCPError(msg, err)
	}
}

func (e *webSFTPAgentToolExecutor) ListDirectory(
	ctx context.Context,
	path string,
	limit int,
) (sessiontools.DirectoryResult, error) {
	var result sessiontools.DirectoryResult
	resolved, err := e.resolvePath(ctx, path)
	if err != nil {
		return result, err
	}
	if limit <= 0 || limit > sessiontools.MaxDirectoryEntries {
		limit = sessiontools.MaxDirectoryEntries
	}
	entries, _, err := e.volume.List(resolved)
	if err != nil {
		return result, err
	}
	result.Path = path
	result.Truncated = len(entries) > limit
	if len(entries) > limit {
		entries = entries[:limit]
	}
	result.Entries = make([]sessiontools.FileEntry, 0, len(entries))
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return sessiontools.DirectoryResult{}, err
		}
		result.Entries = append(
			result.Entries,
			agentToolFileEntry(filepath.Join(path, entry.Name), entry),
		)
	}
	return result, nil
}

func (e *webSFTPAgentToolExecutor) Stat(
	ctx context.Context,
	path string,
) (sessiontools.FileEntry, error) {
	resolved, err := e.resolvePath(ctx, path)
	if err != nil {
		return sessiontools.FileEntry{}, err
	}
	entry, err := e.volume.Stat(resolved)
	if err != nil {
		return sessiontools.FileEntry{}, err
	}
	return agentToolFileEntry(path, entry), nil
}

func (e *webSFTPAgentToolExecutor) ReadText(
	ctx context.Context,
	path string,
	limit int64,
) (sessiontools.TextResult, error) {
	var result sessiontools.TextResult
	if err := requireFilePermission(e.canDownload); err != nil {
		return result, err
	}
	resolved, err := e.resolvePath(ctx, path)
	if err != nil {
		return result, err
	}
	info, err := e.volume.Stat(resolved)
	if err != nil {
		return result, err
	}
	entry := agentToolFileEntry(path, info)
	if entry.IsDir {
		return result, fmt.Errorf("cannot read directory %q as text", path)
	}
	if limit <= 0 || limit > sessiontools.MaxTextBytes {
		limit = sessiontools.MaxTextBytes
	}
	if entry.Size > limit {
		return result, fmt.Errorf("file exceeds the agent tool text limit")
	}
	file, err := e.volume.GetFile(resolved)
	if err != nil {
		return result, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, limit+1))
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
		return result, fmt.Errorf("file tool can read UTF-8 text files only")
	}
	version := entry.Version
	if !truncated {
		digest := sha256.Sum256(data)
		version = "sha256:" + hex.EncodeToString(digest[:])
	}
	return sessiontools.TextResult{
		Path: path, Exists: true, Content: string(data), Version: version,
		Truncated: truncated,
	}, nil
}

func (e *webSFTPAgentToolExecutor) SaveText(
	ctx context.Context,
	path, content, expectedVersion string,
) (sessiontools.FileEntry, error) {
	if err := requireFilePermission(e.canUpload); err != nil {
		return sessiontools.FileEntry{}, err
	}
	if len(content) > sessiontools.MaxTextBytes || !utf8.ValidString(content) ||
		strings.IndexByte(content, 0) >= 0 {
		return sessiontools.FileEntry{}, fmt.Errorf("invalid file tool text content")
	}
	if err := e.check(ctx); err != nil {
		return sessiontools.FileEntry{}, err
	}
	volumeExpectedVersion := expectedVersion
	if expectedVersion == sessiontools.ExpectedVersionAbsent {
		volumeExpectedVersion = webSftpAbsentVersion
	}
	entry, err := e.volume.SaveFile(
		ctx, path,
		bytes.NewReader([]byte(content)),
		int64(len(content)),
		fileMutationOptions{expectedVersion: &volumeExpectedVersion, confined: true},
	)
	if err != nil {
		return sessiontools.FileEntry{}, err
	}
	return agentToolFileEntry(path, entry), nil
}

func (e *webSFTPAgentToolExecutor) Mkdir(ctx context.Context, path string) error {
	if err := requireFilePermission(e.canUpload); err != nil {
		return err
	}
	if err := e.check(ctx); err != nil {
		return err
	}
	return e.volume.MakeDir(ctx, path, fileMutationOptions{confined: true})
}

func (e *webSFTPAgentToolExecutor) Rename(ctx context.Context, path, destinationPath, expectedVersion string) error {
	if err := requireFilePermission(e.canUpload); err != nil {
		return err
	}
	if err := e.check(ctx); err != nil {
		return err
	}
	if filepath.Clean(filepath.Dir(path)) != filepath.Clean(filepath.Dir(destinationPath)) {
		return fmt.Errorf("rename destination must remain in the same directory")
	}
	return e.volume.Rename(ctx, path, destinationPath, fileMutationOptions{expectedVersion: &expectedVersion, confined: true})
}

func (e *webSFTPAgentToolExecutor) Delete(ctx context.Context, path, expectedVersion string, recursive bool) error {
	if err := requireFilePermission(e.canDelete); err != nil {
		return err
	}
	if err := e.check(ctx); err != nil {
		return err
	}
	return e.volume.Remove(ctx, path, recursive, fileMutationOptions{expectedVersion: &expectedVersion, confined: true})
}

func requireFilePermission(allowed bool) error {
	if !allowed {
		return fmt.Errorf("permission denied")
	}
	return nil
}

func (e *webSFTPAgentToolExecutor) check(ctx context.Context) error {
	if e.volume == nil {
		return fmt.Errorf("SFTP volume is unavailable")
	}
	if err := e.volume.check(ctx); err != nil {
		return err
	}
	if e.guard != nil {
		return e.guard()
	}
	return nil
}

func (e *webSFTPAgentToolExecutor) resolvePath(ctx context.Context, path string) (string, error) {
	if err := e.check(ctx); err != nil {
		return "", err
	}
	return e.volume.conn.ResolveAgentToolPath(path)
}

func agentToolFileEntry(path string, info FileInfo) sessiontools.FileEntry {
	return sessiontools.FileEntry{
		Name: info.Name, Path: path, Exists: true, Size: info.Size,
		Perm: info.Perm, ModTime: info.ModTime, IsDir: info.IsDir, Version: info.Version,
	}
}
