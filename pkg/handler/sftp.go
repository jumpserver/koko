package handler

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/gliderlabs/ssh"
	"github.com/pkg/sftp"
	gossh "golang.org/x/crypto/ssh"

	"cocogo/pkg/cctx"
	"cocogo/pkg/config"
	"cocogo/pkg/logger"
	"cocogo/pkg/model"
	"cocogo/pkg/service"
	"cocogo/pkg/srvconn"
)

func SftpHandler(sess ssh.Session) {
	ctx, cancel := cctx.NewContext(sess)
	defer cancel()

	handler := &sftpHandler{user: ctx.User()}
	handler.initial()
	handlers := sftp.Handlers{
		FileGet:  handler,
		FilePut:  handler,
		FileCmd:  handler,
		FileList: handler,
	}

	req := sftp.NewRequestServer(sess, handlers)
	if err := req.Serve(); err == io.EOF {
		_ = req.Close()
		hosts := handler.hosts
		for hostname, dir := range hosts {
			for name, d := range dir.suMaps {
				srvconn.RecycleClient(d.conn)
				delete(dir.suMaps, name)
			}
			delete(hosts, hostname)
		}
		logger.Info("sftp client exited session.")
	} else if err != nil {
		logger.Error("sftp server completed with error:", err)
	}
}

type sftpHandler struct {
	user     *model.User
	assets   model.AssetList
	rootPath string //  tmp || home || ~
	hosts    map[string]*HostNameDir
}

func (fs *sftpHandler) initial() {
	fs.loadAssets()
	fs.hosts = make(map[string]*HostNameDir)
	fs.rootPath = config.GetConf().SftpRoot
	for i, item := range fs.assets {
		tmpDir := &HostNameDir{
			rootPath: fs.rootPath,
			hostname: item.Hostname,
			asset:    &fs.assets[i],
			time:     time.Now().UTC(),
		}
		fs.hosts[item.Hostname] = tmpDir
	}
}

func (fs *sftpHandler) loadAssets() {
	fs.assets = service.GetUserAssets(fs.user.ID, "1")
}

func (fs *sftpHandler) Filelist(r *sftp.Request) (sftp.ListerAt, error) {
	var fileInfos = listerat{}
	var err error
	logger.Debug("list path: ", r.Filepath)
	if r.Filepath == "/" {
		for _, v := range fs.hosts {
			fileInfos = append(fileInfos, v)
		}
		logger.Debug(fileInfos)
		return fileInfos, err
	}
	pathNames := strings.Split(strings.TrimPrefix(r.Filepath, "/"), "/")
	hostDir, ok := fs.hosts[pathNames[0]]
	if !ok {
		return nil, sftp.ErrSshFxNoSuchFile
	}
	if hostDir.suMaps == nil {
		hostDir.suMaps = make(map[string]*SysUserDir)
		systemUsers := hostDir.asset.SystemUsers
		for i, sysUser := range systemUsers {
			hostDir.suMaps[sysUser.Name] = &SysUserDir{
				time:       time.Now().UTC(),
				rootPath:   fs.rootPath,
				systemUser: &systemUsers[i],
				prefix:     fmt.Sprintf("/%s/%s", hostDir.asset.Hostname, sysUser.Name),
			}
		}

	}
	if len(pathNames) == 1 {
		for _, v := range hostDir.suMaps {
			fileInfos = append(fileInfos, v)
		}
		return fileInfos, err
	}

	var realPath string
	var sysUserDir *SysUserDir

	sysUserDir, ok = hostDir.suMaps[pathNames[1]]
	if !ok {
		return nil, sftp.ErrSshFxNoSuchFile
	}
	realPath = sysUserDir.ParsePath(r.Filepath)
	if sysUserDir.client == nil {
		client, conn, err := fs.GetSftpClient(hostDir.asset, sysUserDir.systemUser)
		if err != nil {
			return nil, sftp.ErrSshFxPermissionDenied
		}
		sysUserDir.client = client
		sysUserDir.conn = conn
	}

	switch r.Method {
	case "List":
		logger.Debug("List method")
		fileInfos, err = sysUserDir.client.ReadDir(realPath)
		wraperFiles := make([]os.FileInfo, 0, len(fileInfos))
		for i := 0; i < len(fileInfos); i++ {
			wraperFiles = append(wraperFiles, &wrapperFileInfo{f: fileInfos[i]})
		}
		return listerat(wraperFiles), err
	case "Stat":
		logger.Debug("stat method")
		fsInfo, err := sysUserDir.client.Stat(realPath)
		return listerat([]os.FileInfo{&wrapperFileInfo{f: fsInfo}}), err
	case "Readlink":
		logger.Debug("Readlink method")
		filename, err := sysUserDir.client.ReadLink(realPath)
		fsInfo := &FakeFile{name: filename, modtime: time.Now().UTC()}
		return listerat([]os.FileInfo{fsInfo}), err
	}
	return fileInfos, err
}

func (fs *sftpHandler) Filecmd(r *sftp.Request) error {
	logger.Debug("File cmd: ", r.Filepath)
	pathNames := strings.Split(strings.TrimPrefix(r.Filepath, "/"), "/")
	if len(pathNames) <= 2 {
		return sftp.ErrSshFxPermissionDenied
	}
	hostDir := fs.hosts[pathNames[0]]
	suDir := hostDir.suMaps[pathNames[1]]
	if suDir.client == nil {
		client, conn, err := fs.GetSftpClient(hostDir.asset, suDir.systemUser)
		if err != nil {
			return sftp.ErrSshFxPermissionDenied
		}
		suDir.client = client
		suDir.conn = conn
	}
	realPathName := suDir.ParsePath(r.Filepath)
	switch r.Method {
	case "Setstat":
		return nil
	case "Rename":
		realNewName := suDir.ParsePath(r.Target)
		return suDir.client.Rename(realPathName, realNewName)
	case "Rmdir":
		return suDir.client.RemoveDirectory(realPathName)
	case "Remove":
		return suDir.client.Remove(realPathName)
	case "Mkdir":
		return suDir.client.MkdirAll(realPathName)
	case "Symlink":
		realNewName := suDir.ParsePath(r.Target)
		return suDir.client.Symlink(realPathName, realNewName)
	}
	return nil
}

func (fs *sftpHandler) Filewrite(r *sftp.Request) (io.WriterAt, error) {
	logger.Debug("File write: ", r.Filepath)
	pathNames := strings.Split(strings.TrimPrefix(r.Filepath, "/"), "/")
	if len(pathNames) <= 2 {
		return nil, sftp.ErrSshFxPermissionDenied
	}
	hostDir := fs.hosts[pathNames[0]]
	suDir := hostDir.suMaps[pathNames[1]]
	if suDir.client == nil {
		client, conn, err := fs.GetSftpClient(hostDir.asset, suDir.systemUser)
		if err != nil {
			return nil, sftp.ErrSshFxPermissionDenied
		}
		suDir.client = client
		suDir.conn = conn
	}
	realPathName := suDir.ParsePath(r.Filepath)
	f, err := suDir.client.Create(realPathName)
	return NewWriterAt(f), err
}

func (fs *sftpHandler) Fileread(r *sftp.Request) (io.ReaderAt, error) {
	logger.Debug("File read: ", r.Filepath)
	pathNames := strings.Split(strings.TrimPrefix(r.Filepath, "/"), "/")
	if len(pathNames) <= 2 {
		return nil, sftp.ErrSshFxPermissionDenied
	}
	hostDir := fs.hosts[pathNames[0]]
	suDir := hostDir.suMaps[pathNames[1]]
	if suDir.client == nil {
		ftpClient, client, err := fs.GetSftpClient(hostDir.asset, suDir.systemUser)
		if err != nil {
			return nil, sftp.ErrSshFxPermissionDenied
		}
		suDir.client = ftpClient
		suDir.conn = client
	}
	realPathName := suDir.ParsePath(r.Filepath)
	f, err := suDir.client.Open(realPathName)
	if err != nil {
		return nil, err
	}
	return NewReaderAt(f), err
}

func (fs *sftpHandler) GetSftpClient(asset *model.Asset, sysUser *model.SystemUser) (sftpClient *sftp.Client, sshClient *gossh.Client, err error) {
	sshClient, err = srvconn.NewClient(fs.user, asset, sysUser, config.GetConf().SSHTimeout*time.Second)
	if err != nil {
		return
	}
	sftpClient, err = sftp.NewClient(sshClient)
	if err != nil {
		return
	}
	return sftpClient, sshClient, nil
}

type HostNameDir struct {
	rootPath string
	hostname string
	time     time.Time
	asset    *model.Asset
	suMaps   map[string]*SysUserDir
}

func (h *HostNameDir) Name() string { return h.hostname }

func (h *HostNameDir) Size() int64 { return int64(0) }

func (h *HostNameDir) Mode() os.FileMode { return os.FileMode(0400) | os.ModeDir }

func (h *HostNameDir) ModTime() time.Time { return h.time }

func (h *HostNameDir) IsDir() bool { return true }

func (h *HostNameDir) Sys() interface{} {
	// fake current dir sys info
	fakeInfo, _ := os.Stat(".")
	return fakeInfo.Sys()
}

type SysUserDir struct {
	ID         string
	prefix     string
	rootPath   string
	systemUser *model.SystemUser
	time       time.Time
	client     *sftp.Client
	conn       *gossh.Client
}

func (su *SysUserDir) Name() string { return su.systemUser.Name }

func (su *SysUserDir) Size() int64 { return int64(0) }

func (su *SysUserDir) Mode() os.FileMode { return os.FileMode(0400) | os.ModeDir }

func (su *SysUserDir) ModTime() time.Time { return su.time }

func (su *SysUserDir) IsDir() bool { return true }

func (su *SysUserDir) Sys() interface{} {
	// fake current dir sys info
	fakeInfo, _ := os.Stat(".")
	return fakeInfo.Sys()
}

func (su *SysUserDir) ParsePath(path string) string {
	var realPath string
	realPath = strings.ReplaceAll(path, su.prefix, su.rootPath)
	logger.Debug("real path: ", realPath)
	return realPath

}

type FakeFile struct {
	name    string
	modtime time.Time
	symlink string
}

func (f *FakeFile) Name() string { return f.name }
func (f *FakeFile) Size() int64  { return int64(0) }
func (f *FakeFile) Mode() os.FileMode {
	ret := os.FileMode(0644)
	if f.symlink != "" {
		ret = os.FileMode(0777) | os.ModeSymlink
	}
	return ret
}
func (f *FakeFile) ModTime() time.Time { return f.modtime }
func (f *FakeFile) IsDir() bool        { return false }
func (f *FakeFile) Sys() interface{} {
	fakeInfo, _ := os.Stat(".")
	return fakeInfo.Sys()
}

type wrapperFileInfo struct {
	f os.FileInfo
}

func (w *wrapperFileInfo) Name() string { return w.f.Name() }
func (w *wrapperFileInfo) Size() int64  { return w.f.Size() }
func (w *wrapperFileInfo) Mode() os.FileMode {
	return w.f.Mode()
}
func (w *wrapperFileInfo) ModTime() time.Time { return w.f.ModTime() }
func (w *wrapperFileInfo) IsDir() bool        { return w.f.IsDir() }
func (w *wrapperFileInfo) Sys() interface{} {
	if statInfo, ok := w.f.Sys().(*sftp.FileStat); ok {
		return &syscall.Stat_t{Uid: statInfo.UID, Gid: statInfo.GID}
	} else {
		fakeInfo, _ := os.Stat(".")
		return fakeInfo.Sys()
	}
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

func NewReaderAt(f *sftp.File) io.ReaderAt {
	return &clientReadWritAt{f: f, mu: new(sync.RWMutex)}
}

type clientReadWritAt struct {
	f        *sftp.File
	mu       *sync.RWMutex
	closed   bool
	firstErr error
}

func (c *clientReadWritAt) WriteAt(p []byte, off int64) (n int, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		logger.Debug("WriteAt: ", off)
		return 0, c.firstErr
	}
	nw, err := c.f.Write(p)
	if err != nil {
		c.firstErr = err
		c.closed = true
		_ = c.f.Close()
	}
	return nw, err
}

func (c *clientReadWritAt) ReadAt(p []byte, off int64) (n int, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		logger.Debug("ReadAt: ", off)
		return 0, c.firstErr
	}
	nr, err := c.f.Read(p)
	if err != nil {
		c.firstErr = err
		c.closed = true
		_ = c.f.Close()
	}

	return nr, err
}
