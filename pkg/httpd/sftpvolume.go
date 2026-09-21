package httpd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"

	"github.com/jumpserver-dev/sdk-go/model"
	"github.com/jumpserver/koko/pkg/proxy"
	"github.com/jumpserver/koko/pkg/srvconn"
)

// sftpFileSystem is the remote I/O boundary shared by browser and file tools.
type sftpFileSystem interface {
	ReadDir(string) ([]os.FileInfo, error)
	ReadDirWithCurrentPath(string) ([]os.FileInfo, string, error)
	Stat(string) (os.FileInfo, error)
	Lstat(string) (os.FileInfo, error)
	MkdirExact(string) error
	Rename(string, string) error
	Remove(string) error
	RemoveDirectory(string) error
	Create(string) (*srvconn.SftpFile, error)
	CreateEditorTemp(string, string) (*srvconn.SftpFile, error)
	Open(string) (*srvconn.SftpFile, error)
	OpenUploadTemp(string, bool) (*srvconn.SftpFile, error)
	CommitUploadTemp(string, string, bool) (*model.FTPLog, error)
	OpenForChecksum(string) (*srvconn.SftpFile, error)
	AtomicCreate(string, string) error
	AtomicReplace(string, string) error
	DiscardUploadTemp(string) error
	ResolveAgentToolPath(string) (string, error)
	ValidateAgentToolConfinement() error
	SetOnSessionClosed(func())
	Close()
}

// sftpVolume owns remote files and auditing for one WebSFTP connection.
// All paths use UserSftpConn's namespace; there is no UI-specific base path.
type sftpVolume struct {
	conn      sftpFileSystem
	recorder  *proxy.FTPFileRecorder
	lock      sync.Mutex
	uploads   map[int]*sftpUpload
	closed    atomic.Bool
	closeOnce sync.Once

	transferRead  *cachedTransferFile
	transferWrite *cachedTransferFile

	openRead  func(string) (transferIO, error)
	openWrite func(string, bool) (transferIO, error)
}

type sftpUpload struct {
	path string
	file *srvconn.SftpFile
}

type transferIO interface {
	ReadAt([]byte, int64) (int, error)
	WriteAt([]byte, int64) (int, error)
	Stat() (os.FileInfo, error)
	Close() error
}

type cachedTransferFile struct {
	id, path  string
	file      transferIO
	committed int64
}

type fileMutationOptions struct {
	expectedVersion *string
	force           bool
	confined        bool
}

func newSFTPVolume(conn sftpFileSystem, recorder *proxy.FTPFileRecorder) *sftpVolume {
	return &sftpVolume{conn: conn, recorder: recorder, uploads: make(map[int]*sftpUpload)}
}

func (u *sftpVolume) Close() {
	u.closeOnce.Do(func() {
		u.closed.Store(true)
		// Closing the transport releases operations blocked in remote I/O.
		u.conn.Close()
		u.lock.Lock()
		defer u.lock.Unlock()
		u.closeTransferReadLocked()
		u.closeTransferWriteLocked()
		for id, upload := range u.uploads {
			_ = upload.file.Close()
			u.discardFileRecord(upload.file)
			delete(u.uploads, id)
		}
	})
}

func (u *sftpVolume) check(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if u.closed.Load() {
		return os.ErrClosed
	}
	return nil
}

func (u *sftpVolume) Stat(path string) (FileInfo, error) {
	if u.closed.Load() {
		return FileInfo{}, os.ErrClosed
	}
	info, err := u.conn.Stat(path)
	if err != nil {
		return FileInfo{}, err
	}
	return newWebSftpFileInfo(info), nil
}

// Resolve mutations under the operation lock. Resolving the parent retains
// the directory entry itself when the target is a symlink.
func (u *sftpVolume) mutationPaths(ctx context.Context, confined bool, paths ...string) ([]string, error) {
	if err := u.check(ctx); err != nil {
		return nil, err
	}
	if !confined {
		return paths, nil
	}
	resolved := make([]string, 0, len(paths))
	for _, path := range paths {
		if _, err := u.conn.ResolveAgentToolPath(path); err != nil {
			return nil, err
		}
		parent, err := u.conn.ResolveAgentToolPath(filepath.Dir(path))
		if err != nil {
			return nil, err
		}
		resolved = append(resolved, filepath.Join(parent, filepath.Base(path)))
	}
	return resolved, nil
}

func (u *sftpVolume) MakeDir(ctx context.Context, path string, options fileMutationOptions) error {
	u.lock.Lock()
	defer u.lock.Unlock()
	paths, err := u.mutationPaths(ctx, options.confined, path)
	if err != nil {
		return err
	}
	if _, err := u.conn.Stat(paths[0]); err == nil {
		return os.ErrExist
	} else if !isSftpNotExist(err) {
		return err
	}
	if err := u.check(ctx); err != nil {
		return err
	}
	return u.conn.MkdirExact(paths[0])
}

func (u *sftpVolume) Rename(ctx context.Context, source, destination string, options fileMutationOptions) error {
	u.lock.Lock()
	defer u.lock.Unlock()
	paths, err := u.mutationPaths(ctx, options.confined, source, destination)
	if err != nil {
		return err
	}
	if options.expectedVersion != nil {
		if err := u.verifyExpectedVersion(paths[0], *options.expectedVersion, true); err != nil {
			return err
		}
	}
	if _, err := u.conn.Stat(paths[1]); err == nil {
		return os.ErrExist
	} else if !isSftpNotExist(err) {
		return err
	}
	if err := u.check(ctx); err != nil {
		return err
	}
	return u.conn.Rename(paths[0], paths[1])
}

func (u *sftpVolume) Remove(ctx context.Context, path string, recursive bool, options fileMutationOptions) error {
	u.lock.Lock()
	defer u.lock.Unlock()
	paths, err := u.mutationPaths(ctx, options.confined, path)
	if err != nil {
		return err
	}
	path = paths[0]
	if options.expectedVersion != nil {
		if err := u.verifyExpectedVersion(path, *options.expectedVersion, true); err != nil {
			return err
		}
	}
	info, err := u.conn.Lstat(path)
	if err != nil {
		return err
	}
	if err := u.check(ctx); err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink == 0 && info.IsDir() {
		if !recursive {
			return fmt.Errorf("recursive=true is required to delete a directory")
		}
		return u.conn.RemoveDirectory(path)
	}
	return u.conn.Remove(path)
}

func (u *sftpVolume) discardFileRecord(file *srvconn.SftpFile) {
	if u.recorder != nil && file.FTPLog != nil {
		u.recorder.DiscardFTPFile(file.FTPLog.ID)
	}
}

func (u *sftpVolume) finishFileRecord(file *srvconn.SftpFile) {
	if u.recorder != nil && file.FTPLog != nil {
		u.recorder.FinishFTPFile(file.FTPLog.ID)
	}
}
