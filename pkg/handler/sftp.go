package handler

import (
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gliderlabs/ssh"
	"github.com/pkg/sftp"

	gossh "golang.org/x/crypto/ssh"

	"cocogo/pkg/cctx"
	"cocogo/pkg/config"
	"cocogo/pkg/logger"
	"cocogo/pkg/model"
	"cocogo/pkg/service"
)

func SftpHandler(sess ssh.Session) {
	ctx, cancel := cctx.NewContext(sess)
	defer cancel()

	userhandler := &userSftpRequests{user: ctx.User()}
	userhandler.initial()
	hs := sftp.Handlers{
		FileGet:  userhandler,
		FilePut:  userhandler,
		FileCmd:  userhandler,
		FileList: userhandler}

	req := sftp.NewRequestServer(sess, hs)
	if err := req.Serve(); err == io.EOF {
		_ = req.Close()
		logger.Info("sftp client exited session.")
	} else if err != nil {
		logger.Error("sftp server completed with error:", err)
	}
}

type userSftpRequests struct {
	user     *model.User
	assets   model.AssetList
	rootPath string //  tmp || home || ~
	hosts    map[string]*HostNameDir
}

func (fs *userSftpRequests) initial() {

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

func (fs *userSftpRequests) loadAssets() {
	fs.assets = service.GetUserAssets(fs.user.ID, "1")
}

func (fs *userSftpRequests) Filelist(r *sftp.Request) (sftp.ListerAt, error) {
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
		sysUserDir.client, err = fs.GetSftpClient(hostDir.asset, sysUserDir.systemUser)
		if err != nil {
			return nil, sftp.ErrSshFxPermissionDenied
		}
	}

	fileInfos, err = sysUserDir.client.ReadDir(realPath)

	switch r.Method {
	case "List":
		return fileInfos, err
	case "Stat":
		return fileInfos, err
	case "Readlink":
		return fileInfos, err
	}
	return fileInfos, err
}

func (fs *userSftpRequests) Filecmd(r *sftp.Request) error {
	logger.Debug("File cmd: ", r.Filepath)
	pathNames := strings.Split(strings.TrimPrefix(r.Filepath, "/"), "/")
	if len(pathNames) <= 2 {
		return sftp.ErrSshFxPermissionDenied
	}
	hostDir := fs.hosts[pathNames[0]]
	suDir := hostDir.suMaps[pathNames[1]]
	if suDir.client == nil {
		client, err := fs.GetSftpClient(hostDir.asset, suDir.systemUser)
		if err != nil {
			return sftp.ErrSshFxPermissionDenied
		}
		suDir.client = client
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

func (fs *userSftpRequests) Filewrite(r *sftp.Request) (io.WriterAt, error) {
	logger.Debug("File write: ", r.Filepath)
	pathNames := strings.Split(strings.TrimPrefix(r.Filepath, "/"), "/")
	if len(pathNames) <= 2 {
		return nil, sftp.ErrSshFxPermissionDenied
	}
	hostDir := fs.hosts[pathNames[0]]
	suDir := hostDir.suMaps[pathNames[1]]
	if suDir.client == nil {
		client, err := fs.GetSftpClient(hostDir.asset, suDir.systemUser)
		if err != nil {
			return nil, sftp.ErrSshFxPermissionDenied
		}
		suDir.client = client
	}
	realPathName := suDir.ParsePath(r.Filepath)
	f, err := suDir.client.Create(realPathName)
	return NewWriterAt(f), err
}

func (fs *userSftpRequests) Fileread(r *sftp.Request) (io.ReaderAt, error) {
	logger.Debug("File read: ", r.Filepath)
	pathNames := strings.Split(strings.TrimPrefix(r.Filepath, "/"), "/")
	if len(pathNames) <= 2 {
		return nil, sftp.ErrSshFxPermissionDenied
	}
	hostDir := fs.hosts[pathNames[0]]
	suDir := hostDir.suMaps[pathNames[1]]
	if suDir.client == nil {
		client, err := fs.GetSftpClient(hostDir.asset, suDir.systemUser)
		if err != nil {
			return nil, sftp.ErrSshFxPermissionDenied
		}
		suDir.client = client
	}
	realPathName := suDir.ParsePath(r.Filepath)
	f, err := suDir.client.Open(realPathName)
	if err != nil {
		return nil, err
	}
	return NewReaderAt(f), err
}

func (fs *userSftpRequests) GetSftpClient(asset *model.Asset, sysUser *model.SystemUser) (*sftp.Client, error) {
	logger.Debug("Get Sftp Client")
	info := service.GetSystemUserAssetAuthInfo(sysUser.Id, asset.Id)

	return CreateSFTPConn(sysUser.Username, info.Password, info.PrivateKey, asset.Ip, strconv.Itoa(asset.Port))
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
	return &clientReadWritAt{f: f}
}

func NewReaderAt(f *sftp.File) io.ReaderAt {
	return &clientReadWritAt{f: f}
}

type clientReadWritAt struct {
	f *sftp.File
}

func (c *clientReadWritAt) WriteAt(p []byte, off int64) (n int, err error) {
	return c.f.Write(p)
}

func (c *clientReadWritAt) ReadAt(p []byte, off int64) (n int, err error) {
	return c.f.Read(p)
}

func CreateSFTPConn(user, password, privateKey, host, port string) (*sftp.Client, error) {
	authMethods := make([]gossh.AuthMethod, 0)
	if password != "" {
		authMethods = append(authMethods, gossh.Password(password))
	}

	if privateKey != "" {
		if signer, err := gossh.ParsePrivateKey([]byte(privateKey)); err != nil {
			err = fmt.Errorf("parse private key error: %sc", err)
		} else {
			authMethods = append(authMethods, gossh.PublicKeys(signer))
		}
	}
	config := &gossh.ClientConfig{
		User:            user,
		Auth:            authMethods,
		HostKeyCallback: gossh.InsecureIgnoreHostKey(),
		Timeout:         300 * time.Second,
	}
	client, err := gossh.Dial("tcp", net.JoinHostPort(host, port), config)
	if err != nil {
		return nil, err
	}
	return sftp.NewClient(client)
}
