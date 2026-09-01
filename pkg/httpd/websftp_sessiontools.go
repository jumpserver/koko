package httpd

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/jumpserver/koko/internal/agentapi"
	"github.com/jumpserver/koko/internal/sessiontools"
	"github.com/jumpserver/koko/pkg/logger"
	"github.com/jumpserver/koko/pkg/srvconn"
)

type webSFTPAgentToolExecutor struct {
	volume       *UserWebVolume
	guard        func() error
	resolvePath  func(string) (string, error)
	validatePath func(string) error
	canDownload  func() bool
	canUpload    func() bool
	canDelete    func() bool
}

func (h *webSftp) initializeFileTools() {
	if h.volume == nil || h.volume.UserSftp == nil {
		logger.Errorf("SFTP websocket %s MCP file tools unavailable: SFTP resource is unavailable", h.ws.Uuid)
		return
	}
	if err := h.volume.UserSftp.ValidateAgentToolConfinement(); err != nil {
		logger.Infof(
			"SFTP websocket %s MCP file tools disabled: %s",
			h.ws.Uuid, err,
		)
		return
	}
	resourceID := h.ws.Uuid
	contextSnapshot := agentapi.ContextSnapshot{
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
		volume:       h.volume,
		resolvePath:  h.volume.UserSftp.ResolveAgentToolPath,
		validatePath: h.volume.UserSftp.ValidateAgentToolPath,
		guard: func() error {
			if h.sessionExpired() {
				return fmt.Errorf("session expired or not found")
			}
			return nil
		},
		canDownload: func() bool {
			return canDownload
		},
		canUpload: func() bool {
			return canUpload
		},
		canDelete: func() bool {
			return canDelete
		},
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
	if h.trackedSessionExpired() {
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
	resolved, err := e.resolvePaths(ctx, path)
	if err != nil {
		return result, err
	}
	if limit <= 0 || limit > sessiontools.MaxDirectoryEntries {
		limit = sessiontools.MaxDirectoryEntries
	}
	entries, err := e.volume.UserSftp.ReadDir(resolved[0])
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
			agentToolFileEntry(filepath.Join(path, entry.Name()), entry),
		)
	}
	return result, nil
}

func (e *webSFTPAgentToolExecutor) Stat(
	ctx context.Context,
	path string,
) (sessiontools.FileEntry, error) {
	resolved, err := e.resolvePaths(ctx, path)
	if err != nil {
		return sessiontools.FileEntry{}, err
	}
	entry, err := e.volume.UserSftp.Stat(resolved[0])
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
	resolved, err := e.resolvePaths(ctx, path)
	if err != nil {
		return result, err
	}
	info, err := e.volume.UserSftp.Stat(resolved[0])
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
	file, _, err := e.volume.Download(resolved[0], false)
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
	resolved, err := e.resolveMutationPaths(ctx, path)
	if err != nil {
		return sessiontools.FileEntry{}, err
	}
	volumeExpectedVersion := expectedVersion
	if expectedVersion == sessiontools.ExpectedVersionAbsent {
		volumeExpectedVersion = webSftpAbsentVersion
	}
	entry, err := e.volume.SaveFile(
		resolved[0],
		bytes.NewReader([]byte(content)),
		int64(len(content)),
		&volumeExpectedVersion,
		false,
	)
	if err != nil {
		return sessiontools.FileEntry{}, err
	}
	size := int64(len(content))
	if parsed, parseErr := parseFileInfoSize(entry.Size); parseErr == nil {
		size = parsed
	}
	return sessiontools.FileEntry{
		Name: entry.Name, Path: path, Exists: true, Size: size, Perm: entry.Perm,
		ModTime: parseFileInfoModTime(entry.ModTime), IsDir: entry.IsDir,
		Version: entry.Version,
	}, nil
}

func (e *webSFTPAgentToolExecutor) Mkdir(ctx context.Context, path string) error {
	if err := requireFilePermission(e.canUpload); err != nil {
		return err
	}
	if _, err := e.resolveMutationPaths(ctx, path); err != nil {
		return err
	}
	if e.volume == nil {
		return fmt.Errorf("SFTP volume is unavailable")
	}
	e.volume.lock.Lock()
	defer e.volume.lock.Unlock()
	resolved, err := e.resolveMutationPaths(ctx, path)
	if err != nil {
		return err
	}
	remotePath := resolved[0]
	if _, err := e.volume.UserSftp.Stat(remotePath); err == nil {
		return ErrWebSftpFileConflict
	} else if !os.IsNotExist(err) {
		return err
	}
	return e.volume.UserSftp.MkdirExact(remotePath)
}

func (e *webSFTPAgentToolExecutor) Rename(
	ctx context.Context,
	path, destinationPath, expectedVersion string,
) error {
	if err := requireFilePermission(e.canUpload); err != nil {
		return err
	}
	if _, err := e.resolveMutationPaths(ctx, path, destinationPath); err != nil {
		return err
	}
	if filepath.Clean(filepath.Dir(path)) != filepath.Clean(filepath.Dir(destinationPath)) {
		return fmt.Errorf("rename destination must remain in the same directory")
	}
	if e.volume == nil {
		return fmt.Errorf("SFTP volume is unavailable")
	}
	e.volume.lock.Lock()
	defer e.volume.lock.Unlock()
	resolved, err := e.resolveMutationPaths(ctx, path, destinationPath)
	if err != nil {
		return err
	}
	remotePath := resolved[0]
	remoteDestination := resolved[1]
	if err := verifyAgentToolVersion(
		e.volume.UserSftp, remotePath, expectedVersion,
	); err != nil {
		return err
	}
	if _, err := e.volume.UserSftp.Stat(remoteDestination); err == nil {
		return ErrWebSftpFileConflict
	} else if !os.IsNotExist(err) {
		return err
	}
	// The SFTP v3 rename primitive is no-overwrite. Unlike PosixRename, a
	// destination created after the check causes this operation to fail.
	return e.volume.UserSftp.Rename(remotePath, remoteDestination)
}

func (e *webSFTPAgentToolExecutor) Delete(
	ctx context.Context,
	path, expectedVersion string,
	recursive bool,
) error {
	if err := requireFilePermission(e.canDelete); err != nil {
		return err
	}
	if _, err := e.resolveMutationPaths(ctx, path); err != nil {
		return err
	}
	if e.volume == nil {
		return fmt.Errorf("SFTP volume is unavailable")
	}
	e.volume.lock.Lock()
	defer e.volume.lock.Unlock()
	resolved, err := e.resolveMutationPaths(ctx, path)
	if err != nil {
		return err
	}
	remotePath := resolved[0]
	info, err := e.volume.UserSftp.Stat(remotePath)
	if err != nil {
		return err
	}
	if webSftpFileVersion(info) != expectedVersion {
		return ErrWebSftpFileConflict
	}
	entryInfo, err := e.volume.UserSftp.Lstat(remotePath)
	if err != nil {
		return err
	}
	if entryInfo.Mode()&os.ModeSymlink != 0 {
		return e.volume.UserSftp.Remove(remotePath)
	}
	if entryInfo.IsDir() {
		if !recursive {
			return fmt.Errorf("recursive=true is required to delete a directory")
		}
		return e.volume.UserSftp.RemoveDirectory(remotePath)
	}
	// Use the file-only primitive selected from the approved, revalidated
	// object. A later file-to-directory swap fails instead of becoming a
	// recursive deletion.
	return e.volume.UserSftp.Remove(remotePath)
}

func verifyAgentToolVersion(
	userSFTP *srvconn.UserSftpConn,
	path, expectedVersion string,
) error {
	info, err := userSFTP.Stat(path)
	if err != nil {
		return err
	}
	if webSftpFileVersion(info) != expectedVersion {
		return ErrWebSftpFileConflict
	}
	return nil
}

func requireFilePermission(check func() bool) error {
	if check != nil && !check() {
		return fmt.Errorf("permission denied")
	}
	return nil
}

func (e *webSFTPAgentToolExecutor) check(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if e.guard != nil {
		return e.guard()
	}
	return nil
}

func (e *webSFTPAgentToolExecutor) resolvePaths(
	ctx context.Context,
	paths ...string,
) ([]string, error) {
	if err := e.check(ctx); err != nil {
		return nil, err
	}
	resolve := e.resolvePath
	if resolve == nil && e.volume != nil && e.volume.UserSftp != nil {
		resolve = e.volume.UserSftp.ResolveAgentToolPath
	}
	validate := e.validatePath
	if validate == nil && e.volume != nil && e.volume.UserSftp != nil {
		validate = e.volume.UserSftp.ValidateAgentToolPath
	}
	if resolve == nil && validate == nil {
		return nil, fmt.Errorf("file tool path resolver is unavailable")
	}
	resolved := make([]string, 0, len(paths))
	for _, path := range paths {
		if resolve != nil {
			value, err := resolve(path)
			if err != nil {
				return nil, err
			}
			resolved = append(resolved, value)
			continue
		}
		if err := validate(path); err != nil {
			return nil, err
		}
		resolved = append(resolved, path)
	}
	return resolved, nil
}

func (e *webSFTPAgentToolExecutor) resolveMutationPaths(
	ctx context.Context,
	paths ...string,
) ([]string, error) {
	resolved := make([]string, 0, len(paths))
	for _, value := range paths {
		if _, err := e.resolvePaths(ctx, value); err != nil {
			return nil, err
		}
		parent, err := e.resolvePaths(ctx, filepath.Dir(value))
		if err != nil {
			return nil, err
		}
		resolved = append(resolved, filepath.Join(parent[0], filepath.Base(value)))
	}
	return resolved, nil
}

func agentToolFileEntry(path string, info os.FileInfo) sessiontools.FileEntry {
	return sessiontools.FileEntry{
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
