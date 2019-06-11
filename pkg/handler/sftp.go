package handler

import (
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/gliderlabs/ssh"
	"github.com/pkg/sftp"
	gossh "golang.org/x/crypto/ssh"

	"github.com/jumpserver/koko/pkg/cctx"
	"github.com/jumpserver/koko/pkg/config"
	"github.com/jumpserver/koko/pkg/logger"
	"github.com/jumpserver/koko/pkg/model"
	"github.com/jumpserver/koko/pkg/service"
	"github.com/jumpserver/koko/pkg/srvconn"
)

func SftpHandler(sess ssh.Session) {
	ctx, cancel := cctx.NewContext(sess)
	defer cancel()
	host, _, _ := net.SplitHostPort(sess.RemoteAddr().String())
	handler := &sftpHandler{user: ctx.User(), addr: host}
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
		handler.Close()
		logger.Info("sftp client exited session.")
	} else if err != nil {
		logger.Error("sftp server completed with error:", err)
	}
}

type sftpHandler struct {
	user     *model.User
	addr     string
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

	if !fs.validatePermission(hostDir.asset.ID, sysUserDir.systemUser.ID, model.ConnectAction) {
		return nil, sftp.ErrSshFxPermissionDenied
	}

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

func (fs *sftpHandler) Filecmd(r *sftp.Request) (err error) {
	logger.Debug("File cmd: ", r.Filepath)
	pathNames := strings.Split(strings.TrimPrefix(r.Filepath, "/"), "/")
	if len(pathNames) <= 2 {
		return sftp.ErrSshFxPermissionDenied
	}
	hostDir := fs.hosts[pathNames[0]]
	suDir := hostDir.suMaps[pathNames[1]]

	if !fs.validatePermission(hostDir.asset.ID, suDir.systemUser.ID, model.ConnectAction) {
		return sftp.ErrSshFxPermissionDenied
	}

	if suDir.client == nil {
		client, conn, err := fs.GetSftpClient(hostDir.asset, suDir.systemUser)
		if err != nil {
			return sftp.ErrSshFxPermissionDenied
		}
		suDir.client = client
		suDir.conn = conn
	}
	realPathName := suDir.ParsePath(r.Filepath)
	logData := &model.FTPLog{
		User:       fs.user.Username,
		Hostname:   hostDir.asset.Hostname,
		OrgID:      hostDir.asset.OrgID,
		SystemUser: suDir.systemUser.Name,
		RemoteAddr: fs.addr,
		Operate:    r.Method,
		Path:       realPathName,
		DataStart:  time.Now().UTC().Format("2006-01-02 15:04:05 +0000"),
		IsSuccess:  false,
	}
	defer fs.CreateFTPLog(logData)
	switch r.Method {
	case "Setstat":
		return
	case "Rename":
		realNewName := suDir.ParsePath(r.Target)
		logData.Path = fmt.Sprintf("%s=>%s", realPathName, realNewName)
		err = suDir.client.Rename(realPathName, realNewName)
	case "Rmdir":
		err = suDir.client.RemoveDirectory(realPathName)
	case "Remove":
		err = suDir.client.Remove(realPathName)
	case "Mkdir":
		err = suDir.client.MkdirAll(realPathName)
	case "Symlink":
		realNewName := suDir.ParsePath(r.Target)
		logData.Path = fmt.Sprintf("%s=>%s", realPathName, realNewName)
		err = suDir.client.Symlink(realPathName, realNewName)
	default:
		return
	}
	if err == nil {
		logData.IsSuccess = true
	}
	return
}

func (fs *sftpHandler) Filewrite(r *sftp.Request) (io.WriterAt, error) {
	logger.Debug("File write: ", r.Filepath)
	pathNames := strings.Split(strings.TrimPrefix(r.Filepath, "/"), "/")
	if len(pathNames) <= 2 {
		return nil, sftp.ErrSshFxPermissionDenied
	}
	hostDir := fs.hosts[pathNames[0]]
	suDir := hostDir.suMaps[pathNames[1]]

	if !fs.validatePermission(hostDir.asset.ID, suDir.systemUser.ID, model.UploadAction) {
		return nil, sftp.ErrSshFxPermissionDenied
	}

	if suDir.client == nil {
		client, conn, err := fs.GetSftpClient(hostDir.asset, suDir.systemUser)
		if err != nil {
			return nil, sftp.ErrSshFxPermissionDenied
		}
		suDir.client = client
		suDir.conn = conn
	}
	realPathName := suDir.ParsePath(r.Filepath)
	logData := &model.FTPLog{
		User:       fs.user.Username,
		Hostname:   hostDir.asset.Hostname,
		OrgID:      hostDir.asset.OrgID,
		SystemUser: suDir.systemUser.Name,
		RemoteAddr: fs.addr,
		Operate:    "Upload",
		Path:       realPathName,
		DataStart:  time.Now().UTC().Format("2006-01-02 15:04:05 +0000"),
		IsSuccess:  false,
	}
	defer fs.CreateFTPLog(logData)
	f, err := suDir.client.Create(realPathName)
	if err == nil {
		logData.IsSuccess = true
	}
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
	if !fs.validatePermission(hostDir.asset.ID, suDir.systemUser.ID, model.DownloadAction) {
		return nil, sftp.ErrSshFxPermissionDenied
	}
	if suDir.client == nil {
		ftpClient, client, err := fs.GetSftpClient(hostDir.asset, suDir.systemUser)
		if err != nil {
			return nil, sftp.ErrSshFxPermissionDenied
		}
		suDir.client = ftpClient
		suDir.conn = client
	}
	realPathName := suDir.ParsePath(r.Filepath)
	logData := &model.FTPLog{
		User:       fs.user.Username,
		Hostname:   hostDir.asset.Hostname,
		OrgID:      hostDir.asset.OrgID,
		SystemUser: suDir.systemUser.Name,
		RemoteAddr: fs.addr,
		Operate:    "Download",
		Path:       realPathName,
		DataStart:  time.Now().UTC().Format("2006-01-02 15:04:05 +0000"),
		IsSuccess:  false,
	}
	defer fs.CreateFTPLog(logData)
	f, err := suDir.client.Open(realPathName)
	if err != nil {
		return nil, err
	}
	logData.IsSuccess = true
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

func (fs *sftpHandler) CreateFTPLog(data *model.FTPLog) {
	for i := 0; i < 4; i++ {
		err := service.PushFTPLog(data)
		if err == nil {
			break
		}
		logger.Debugf("create FTP log err: %s", err.Error())
		time.Sleep(500 * time.Millisecond)
	}
}

func (fs *sftpHandler) Close() {
	for _, dir := range fs.hosts {
		if dir.suMaps == nil {
			continue
		}
		for _, d := range dir.suMaps {
			if d.client != nil {
				_ = d.client.Close()
				srvconn.RecycleClient(d.conn)
			}
		}
	}
}

func (fs *sftpHandler) validatePermission(aid, suid, operate string) bool {
	return service.ValidateUserAssetPermission(
		fs.user.ID, aid, suid, operate,
	)
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
