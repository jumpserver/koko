package handler

import (
	"io"
	"net"
	"os"
	"sync"
	"syscall"
	"time"

	"github.com/gliderlabs/ssh"
	"github.com/pkg/sftp"
	uuid "github.com/satori/go.uuid"

	"github.com/jumpserver/koko/pkg/logger"
	"github.com/jumpserver/koko/pkg/model"
	"github.com/jumpserver/koko/pkg/srvconn"
)

func SftpHandler(sess ssh.Session) {
	currentUser, ok := sess.Context().Value(model.ContextKeyUser).(*model.User)
	if !ok || currentUser.ID == "" {
		logger.Errorf("SFTP User not found, exit.")
		return
	}
	host, _, _ := net.SplitHostPort(sess.RemoteAddr().String())
	userSftp := NewSFTPHandler(currentUser, host)
	handlers := sftp.Handlers{
		FileGet:  userSftp,
		FilePut:  userSftp,
		FileCmd:  userSftp,
		FileList: userSftp,
	}
	reqID := uuid.NewV4().String()
	logger.Infof("SFTP request %s: Handler start", reqID)
	req := sftp.NewRequestServer(sess, handlers)
	if err := req.Serve(); err == io.EOF {
		logger.Debugf("SFTP request %s: Exited session.", reqID)
	} else if err != nil {
		logger.Errorf("SFTP request %s: Server completed with error %s", reqID, err)
	}
	_ = req.Close()
	userSftp.Close()
	logger.Infof("SFTP request %s: Handler exit.", reqID)
}

func NewSFTPHandler(user *model.User, addr string) *sftpHandler {
	return &sftpHandler{UserSftpConn: srvconn.NewUserSftpConn(user, addr)}
}

type sftpHandler struct {
	*srvconn.UserSftpConn
}

func (fs *sftpHandler) Filelist(r *sftp.Request) (sftp.ListerAt, error) {
	switch r.Method {
	case "List":
		logger.Debug("List method: ", r.Filepath)
		res, err := fs.ReadDir(r.Filepath)
		fileInfos := make(listerat, 0, len(res))
		for i := 0; i < len(res); i++ {
			fileInfos = append(fileInfos, &wrapperSFTPFileInfo{f: res[i]})
		}
		return fileInfos, err
	case "Stat":
		logger.Debug("stat method: ", r.Filepath)
		fsInfo, err := fs.Stat(r.Filepath)
		return listerat([]os.FileInfo{fsInfo}), err
	case "Readlink":
		logger.Debug("Readlink method", r.Filepath)
		filename, err := fs.ReadLink(r.Filepath)
		fsInfo := srvconn.NewFakeSymFile(filename)
		return listerat([]os.FileInfo{&wrapperSFTPFileInfo{f: fsInfo}}), err
	}
	return nil, sftp.ErrSshFxOpUnsupported
}

func (fs *sftpHandler) Filecmd(r *sftp.Request) (err error) {
	logger.Debug("File cmd: ", r.Filepath)

	switch r.Method {
	case "Setstat":
		return
	case "Rename":
		logger.Debugf("%s=>%s", r.Filepath, r.Target)
		return fs.Rename(r.Filepath, r.Target)
	case "Rmdir":
		err = fs.RemoveDirectory(r.Filepath)
	case "Remove":
		err = fs.Remove(r.Filepath)
	case "Mkdir":
		err = fs.MkdirAll(r.Filepath)
	case "Symlink":
		logger.Debugf("%s=>%s", r.Filepath, r.Target)
		err = fs.Symlink(r.Filepath, r.Target)
	default:
		return
	}
	return
}

func (fs *sftpHandler) Filewrite(r *sftp.Request) (io.WriterAt, error) {
	logger.Debug("File write: ", r.Filepath)
	f, err := fs.Create(r.Filepath)
	if err != nil {
		return nil, err
	}
	go func() {
		<-r.Context().Done()
		if err := f.Close(); err != nil {
			logger.Errorf("Remote sftp file %s close err: %s", r.Filepath, err)
		}
		logger.Infof("Sftp file write %s done", r.Filepath)
	}()
	return NewWriterAt(f), err
}

func (fs *sftpHandler) Fileread(r *sftp.Request) (io.ReaderAt, error) {
	logger.Debug("File read: ", r.Filepath)
	f, err := fs.Open(r.Filepath)
	if err != nil {
		return nil, err
	}
	go func() {
		<-r.Context().Done()
		if err := f.Close(); err != nil {
			logger.Errorf("Remote sftp file %s close err: %s", r.Filepath, err)
		}
		logger.Infof("Sftp File read %s done", r.Filepath)

	}()
	return f, err
}

func (fs *sftpHandler) Close() {
	fs.UserSftpConn.Close()
}

type listerat []os.FileInfo

func (f listerat) ListAt(ls []os.FileInfo, offset int64) (int, error) {
	var n int
	if offset >= int64(len(f)) {
		return 0, io.EOF
	}
	n = copy(ls, f[offset:])
	if n < len(ls) {
		return n, io.EOF
	}
	return n, nil
}

func NewWriterAt(f *sftp.File) io.WriterAt {
	return &clientReadWritAt{f: f, mu: new(sync.RWMutex)}
}

type clientReadWritAt struct {
	f  *sftp.File
	mu *sync.RWMutex
}

func (c *clientReadWritAt) WriteAt(p []byte, off int64) (n int, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	_, _ = c.f.Seek(off, 0)
	return c.f.Write(p)
}

type wrapperSFTPFileInfo struct {
	f os.FileInfo
}

func (w *wrapperSFTPFileInfo) Name() string {
	return w.f.Name()
}
func (w *wrapperSFTPFileInfo) Size() int64 { return w.f.Size() }
func (w *wrapperSFTPFileInfo) Mode() os.FileMode {
	return w.f.Mode()
}
func (w *wrapperSFTPFileInfo) ModTime() time.Time { return w.f.ModTime() }
func (w *wrapperSFTPFileInfo) IsDir() bool        { return w.f.IsDir() }
func (w *wrapperSFTPFileInfo) Sys() interface{} {
	if statInfo, ok := w.f.Sys().(*sftp.FileStat); ok {
		return &syscall.Stat_t{Uid: statInfo.UID, Gid: statInfo.GID}
	}
	if statInfo, ok := w.f.Sys().(*syscall.Stat_t); ok {
		return &syscall.Stat_t{Uid: statInfo.Uid, Gid: statInfo.Gid}
	}
	return &syscall.Stat_t{Uid: 0, Gid: 0}
}
