package assetexec

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jumpserver-dev/sdk-go/model"
	"github.com/jumpserver/koko/pkg/srvconn"
)

type FileInfo struct {
	Name     string    `json:"name"`
	Path     string    `json:"path"`
	Size     int64     `json:"size"`
	Mode     string    `json:"mode"`
	IsDir    bool      `json:"is_dir"`
	Modified time.Time `json:"modified"`
}

type FileResult struct {
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256,omitempty"`
}

func (c *Connection) openSFTP(ctx context.Context, token *model.ConnectToken) error {
	ctx, cancel := context.WithTimeout(ctx, c.opts.ConnectTimeout)
	defer cancel()
	if c.opts.JMS == nil {
		return errors.New("SFTP requires the authenticated component service")
	}
	cfg, err := c.opts.JMS.GetTerminalConfig()
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	c.sftp = srvconn.NewUserSftpConn(c.opts.JMS,
		srvconn.WithConnectContext(ctx),
		srvconn.WithUser(&token.User), srvconn.WithConnectToken(token),
		srvconn.WithRemoteAddr(c.opts.RemoteAddr), srvconn.WithTerminalCfg(&cfg),
		srvconn.WithLoginFrom(model.LabelField(c.opts.LoginFrom)),
	)
	return c.sftp.ValidateAgentToolConfinement()
}

func (c *Connection) TerminalSessionIDs() []string {
	if c.sftp == nil {
		return nil
	}
	return c.sftp.SessionIDs()
}

func (c *Connection) filePath(ctx context.Context, value string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if c.closed.Load() {
		return "", ErrContextLost
	}
	if c.sftp == nil {
		return "", ErrUnsupported
	}
	if value == "" {
		value = "/"
	}
	if len(value) > 4096 || strings.ContainsRune(value, 0) || !path.IsAbs(value) {
		return "", errors.New("remote path must be an absolute virtual SFTP path")
	}
	return c.sftp.ResolveAgentToolPath(value)
}

func (c *Connection) watchFileContext(ctx context.Context) func() bool {
	return context.AfterFunc(ctx, func() {
		c.closed.Store(true)
		if c.sftp != nil {
			c.sftp.InterruptTransports()
		}
	})
}

// Mutations operate on the validated directory entry, not on a symlink's
// referent. Canonicalize the parent while retaining the original basename.
func (c *Connection) mutationPath(ctx context.Context, value string) (string, error) {
	if _, err := c.filePath(ctx, value); err != nil {
		return "", err
	}
	parent, err := c.filePath(ctx, path.Dir(value))
	if err != nil {
		return "", err
	}
	return path.Join(parent, path.Base(value)), nil
}

func makeFileInfo(value os.FileInfo, name string) FileInfo {
	return FileInfo{Name: value.Name(), Path: name, Size: value.Size(), Mode: value.Mode().String(), IsDir: value.IsDir(), Modified: value.ModTime()}
}

func (c *Connection) ListPage(ctx context.Context, name string, offset, limit int) ([]FileInfo, bool, bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	defer c.watchFileContext(ctx)()
	resolved, err := c.filePath(ctx, name)
	if err != nil {
		return nil, false, false, err
	}
	if limit <= 0 || limit > 1000 {
		limit = 1000
	}
	page, err := c.sftp.ReadDirPage(ctx, resolved, offset, limit)
	if err != nil {
		return nil, false, false, err
	}
	result := make([]FileInfo, 0, len(page.Entries))
	for _, value := range page.Entries {
		result = append(result, makeFileInfo(value, path.Join(resolved, value.Name())))
	}
	return result, page.HasMore, page.Limited, nil
}

func (c *Connection) Stat(ctx context.Context, name string) (FileInfo, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	defer c.watchFileContext(ctx)()
	resolved, err := c.filePath(ctx, name)
	if err != nil {
		return FileInfo{}, err
	}
	value, err := c.sftp.Lstat(resolved)
	if err != nil {
		return FileInfo{}, err
	}
	return makeFileInfo(value, resolved), nil
}

func (c *Connection) ReadFile(ctx context.Context, name string, dst io.Writer) (FileResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	defer c.watchFileContext(ctx)()
	if dst == nil {
		return FileResult{}, errors.New("download destination is required")
	}
	if !c.actions.EnableDownload() {
		return FileResult{}, ErrPermission
	}
	resolved, err := c.filePath(ctx, name)
	if err != nil {
		return FileResult{}, err
	}
	file, err := c.sftp.Open(resolved)
	if err != nil {
		return FileResult{}, err
	}
	closeFile := sync.OnceValue(file.Close)
	defer closeFile()
	info, err := file.Stat()
	if err != nil {
		return FileResult{}, err
	}
	if !info.Mode().IsRegular() || info.Size() > c.opts.MaxFileBytes {
		return FileResult{}, errors.New("download requires a regular file within the transfer limit")
	}
	stop := context.AfterFunc(ctx, func() { _ = closeFile() })
	defer stop()
	hash := sha256.New()
	n, err := io.CopyBuffer(io.MultiWriter(dst, hash), io.LimitReader(file, c.opts.MaxFileBytes+1), make([]byte, 64<<10))
	result := FileResult{Path: resolved, Size: n, SHA256: hex.EncodeToString(hash.Sum(nil))}
	if n > c.opts.MaxFileBytes {
		return result, errors.New("file grew beyond the transfer limit")
	}
	if ctx.Err() != nil {
		return result, ctx.Err()
	}
	if err == nil && n != info.Size() {
		err = errors.New("file changed during download")
	}
	return result, err
}

func (c *Connection) WriteFile(ctx context.Context, name string, src io.Reader, size int64, expectedSHA256 string, overwrite bool) (FileResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	defer c.watchFileContext(ctx)()
	if src == nil {
		return FileResult{}, errors.New("upload source is required")
	}
	if !c.actions.EnableUpload() {
		return FileResult{}, ErrPermission
	}
	if size < 0 || size > c.opts.MaxFileBytes {
		return FileResult{}, errors.New("upload exceeds the transfer limit")
	}
	expectedHash, err := hex.DecodeString(expectedSHA256)
	if err != nil || len(expectedHash) != sha256.Size {
		return FileResult{}, errors.New("upload requires a SHA-256 digest")
	}
	resolved, err := c.mutationPath(ctx, name)
	if err != nil {
		return FileResult{}, err
	}
	if resolved == "/" {
		return FileResult{}, ErrPermission
	}
	if err = c.sftp.CheckUploadSize(resolved, size); err != nil {
		return FileResult{}, err
	}
	temp := path.Join(path.Dir(resolved), ".jms-agent-"+uuid.NewString())
	file, err := c.sftp.OpenUploadTemp(temp, true)
	if err != nil {
		return FileResult{}, err
	}
	closeFile := sync.OnceValue(file.Close)
	defer func() { _ = closeFile(); _ = c.sftp.DiscardUploadTemp(temp) }()
	stop := context.AfterFunc(ctx, func() { _ = closeFile() })
	defer stop()
	hash := sha256.New()
	buffer := make([]byte, 64<<10)
	var copied int64
	for copied < size {
		if err = ctx.Err(); err != nil {
			return FileResult{}, err
		}
		chunk := buffer[:min(int64(len(buffer)), size-copied)]
		n, readErr := io.ReadFull(src, chunk)
		if n > 0 {
			written, writeErr := file.WriteAt(chunk[:n], copied)
			_, _ = hash.Write(chunk[:written])
			copied += int64(written)
			if writeErr != nil {
				return FileResult{}, writeErr
			}
			if written != n {
				return FileResult{}, io.ErrShortWrite
			}
		}
		if readErr != nil {
			return FileResult{}, readErr
		}
	}
	var extra [1]byte
	if n, readErr := src.Read(extra[:]); n != 0 || readErr != io.EOF {
		return FileResult{}, errors.New("upload size does not match request")
	}
	actual := hex.EncodeToString(hash.Sum(nil))
	if actual != strings.ToLower(expectedSHA256) {
		return FileResult{}, errors.New("upload digest mismatch")
	}
	if err = closeFile(); err != nil {
		return FileResult{}, err
	}
	if err = ctx.Err(); err != nil {
		return FileResult{}, err
	}
	// Revalidate immediately before committing a mutation to the final name.
	if again, resolveErr := c.mutationPath(ctx, name); resolveErr != nil || again != resolved {
		return FileResult{}, errors.New("upload destination changed")
	}
	if _, err = c.sftp.CommitUploadTemp(temp, resolved, overwrite); err != nil {
		return FileResult{}, err
	}
	return FileResult{Path: resolved, Size: copied, SHA256: actual}, nil
}

func (c *Connection) Mkdir(ctx context.Context, name string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	defer c.watchFileContext(ctx)()
	if !c.actions.EnableUpload() {
		return ErrPermission
	}
	resolved, err := c.mutationPath(ctx, name)
	if err != nil {
		return err
	}
	if resolved == "/" {
		return ErrPermission
	}
	return c.sftp.MkdirExact(resolved)
}

func (c *Connection) Rename(ctx context.Context, source, destination string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	defer c.watchFileContext(ctx)()
	if !c.actions.EnableUpload() {
		return ErrPermission
	}
	src, err := c.mutationPath(ctx, source)
	if err != nil {
		return err
	}
	dst, err := c.mutationPath(ctx, destination)
	if err != nil {
		return err
	}
	if src == "/" || dst == "/" {
		return ErrPermission
	}
	return c.sftp.Rename(src, dst)
}

func (c *Connection) Remove(ctx context.Context, name string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	defer c.watchFileContext(ctx)()
	if !c.actions.EnableDelete() {
		return ErrPermission
	}
	resolved, err := c.mutationPath(ctx, name)
	if err != nil {
		return err
	}
	if resolved == "/" {
		return ErrPermission
	}
	info, err := c.sftp.Lstat(resolved)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return c.sftp.RemoveDirectoryExact(resolved)
	}
	if !info.Mode().IsRegular() && info.Mode()&os.ModeSymlink == 0 {
		return fmt.Errorf("unsupported file type")
	}
	return c.sftp.Remove(resolved)
}
