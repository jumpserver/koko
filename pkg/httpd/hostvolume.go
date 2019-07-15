package httpd

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/LeeEirc/elfinder"
	"github.com/pkg/sftp"

	"github.com/jumpserver/koko/pkg/common"
	"github.com/jumpserver/koko/pkg/config"
	"github.com/jumpserver/koko/pkg/logger"
	"github.com/jumpserver/koko/pkg/model"
	"github.com/jumpserver/koko/pkg/service"
	"github.com/jumpserver/koko/pkg/srvconn"
)

func NewHostVolume(user *model.User, asset *model.Asset, addr string) *hostVolume{

	homeName := asset.Hostname
	if asset.OrgID != ""{
		homeName = fmt.Sprintf("%s.%s",asset.Hostname, asset.OrgName)
	}
	rawID := fmt.Sprintf("%s@%s@%s", user.Username, homeName, addr)

	hV := &hostVolume{
		Uuid:elfinder.GenerateID(rawID),
		Addr:addr,
		Name:homeName,
		basePath:fmt.Sprintf("/%s",homeName),
		user:user,
		asset:asset,
	}
	hV.initial()
	return hV
}


type hostVolume struct {
	Uuid     string
	Addr     string
	Name     string
	basePath string
	user     *model.User
	asset   *model.Asset

	rootPath string //  tmp || home || ~
	suMaps   map[string]*sysUserVolume
	time     time.Time

	localTmpPath string
	permCache map[string]bool
}


func (h *hostVolume) initial(){
	conf := config.GetConf()
	h.rootPath = conf.SftpRoot
	h.localTmpPath = filepath.Join(conf.RootPath, "data", "tmp")
	h.permCache = make(map[string]bool)
	h.suMaps = make(map[string]*sysUserVolume)
	systemUsers := h.asset.SystemUsers
	for i, sysUser := range systemUsers {
		h.suMaps[sysUser.Name] = &sysUserVolume{
			VID:        h.ID(),
			hostpath:   h.basePath,
			suPath:     filepath.Join(h.basePath, sysUser.Name),
			systemUser: &systemUsers[i],
			rootPath:   h.rootPath,
		}
	}
}

func (h *hostVolume) ID() string {
	return h.Uuid
}

func (h *hostVolume) Info(path string) (elfinder.FileDir, error) {
	var rest elfinder.FileDir
	if path == "" || path == "/" {
		path = h.basePath
	}
	if path == h.basePath {
		return h.RootFileDir(), nil
	}
	pathNames := strings.Split(strings.TrimPrefix(path, "/"), "/")
	suVol, ok := h.suMaps[pathNames[1]]
	if !ok {
		return rest, os.ErrNotExist
	}

	if path == suVol.suPath{
		return suVol.info(),nil
	}

	if !h.validatePermission(h.asset.ID, suVol.systemUser.ID, model.ConnectAction) {
		return rest, os.ErrPermission
	}

	if suVol.client == nil {
		sftClient, conn, err := h.GetSftpClient(h.asset, suVol.systemUser)
		if err != nil {
			return rest, os.ErrPermission
		}
		suVol.homeDirPath, err = sftClient.Getwd()
		if err != nil {
			return rest, err
		}
		suVol.client = sftClient
		suVol.conn = conn

	}

	realPath := suVol.ParsePath(path)
	dirname := filepath.Dir(path)
	fileInfos, err := suVol.client.Stat(realPath)
	if err != nil {
		return rest, err
	}
	rest.Name = fileInfos.Name()
	rest.Hash = hashPath(h.ID(), path)
	rest.Phash = hashPath(h.ID(), dirname)
	rest.Size = fileInfos.Size()
	rest.Volumeid = h.ID()
	if fileInfos.IsDir() {
		rest.Mime = "directory"
		rest.Dirs = 1
	} else {
		rest.Mime = "file"
		rest.Dirs = 0
	}
	rest.Read, rest.Write = elfinder.ReadWritePem(fileInfos.Mode())
	return rest, nil
}

func (h *hostVolume) List(path string) []elfinder.FileDir {
	var dirs []elfinder.FileDir
	if path == "" || path == "/" {
		path = h.basePath
	}
	if path == h.basePath {
		dirs = make([]elfinder.FileDir, 0, len(h.suMaps))
		for _, item := range h.suMaps {
			dirs = append(dirs, item.info())
		}
		return dirs
	}
	pathNames := strings.Split(strings.TrimPrefix(path, "/"), "/")
	sysUserVol, ok := h.suMaps[pathNames[1]]
	if !ok {
		return dirs
	}
	if sysUserVol.client == nil {
		sftClient, conn, err := h.GetSftpClient(h.asset, sysUserVol.systemUser)
		if err != nil {
			return dirs
		}
		sysUserVol.homeDirPath, err = sftClient.Getwd()
		if err != nil {
			return dirs
		}
		sysUserVol.client = sftClient
		sysUserVol.conn = conn
	}
	realPath := sysUserVol.ParsePath(path)
	subFiles, err := sysUserVol.client.ReadDir(realPath)
	if err != nil {
		return dirs
	}
	dirs = make([]elfinder.FileDir, 0, len(subFiles))
	for _, fInfo := range subFiles {
		fileDir, err := h.Info(filepath.Join(path, fInfo.Name()))
		if err != nil {
			continue
		}
		dirs = append(dirs, fileDir)
	}
	return dirs
}

func (h *hostVolume) Parents(path string, dep int) []elfinder.FileDir {
	relativepath := strings.TrimPrefix(strings.TrimPrefix(path, h.basePath), "/")
	relativePaths := strings.Split(relativepath, "/")
	dirs := make([]elfinder.FileDir, 0, len(relativePaths))
	for i := range relativePaths {
		realDirPath := filepath.Join(h.basePath, filepath.Join(relativePaths[:i]...))
		result, err := h.Info(realDirPath)
		if err != nil {
			continue
		}
		dirs = append(dirs, result)
		tmpDir := h.List(realDirPath)
		for j, item := range tmpDir {
			if item.Dirs == 1 {
				dirs = append(dirs, tmpDir[j])
			}
		}
	}
	return dirs
}

func (h *hostVolume) GetFile(path string) (reader io.ReadCloser, err error) {
	pathNames := strings.Split(strings.TrimPrefix(path, "/"), "/")
	sysUserVol, ok := h.suMaps[pathNames[1]]
	if !ok {
		return nil, os.ErrNotExist
	}
	if !h.validatePermission(h.asset.ID, sysUserVol.systemUser.ID, model.DownloadAction) {
		return nil, os.ErrPermission
	}
	if sysUserVol.client == nil {
		sftClient, conn, err := h.GetSftpClient(h.asset, sysUserVol.systemUser)
		if err != nil {
			return nil, os.ErrPermission
		}
		sysUserVol.homeDirPath, err = sftClient.Getwd()
		if err != nil {
			return nil, err
		}
		sysUserVol.client = sftClient
		sysUserVol.conn = conn

	}
	realPath := sysUserVol.ParsePath(path)
	logData := &model.FTPLog{
		User:       fmt.Sprintf("%s (%s)", h.user.Name, h.user.Username),
		Hostname:   h.asset.Hostname,
		OrgID:      h.asset.OrgID,
		SystemUser: sysUserVol.systemUser.Name,
		RemoteAddr: h.Addr,
		Operate:    "Download",
		Path:       realPath,
		DataStart:  common.CurrentUTCTime(),
		IsSuccess:  false,
	}
	defer h.CreateFTPLog(logData)
	reader, err = sysUserVol.client.Open(realPath)
	if err != nil {
		return
	}
	logData.IsSuccess = true
	return
}

func (h *hostVolume) UploadFile(dir, filename string, reader io.Reader) (elfinder.FileDir, error) {
	var rest elfinder.FileDir
	var err error
	if dir == "" || dir == "/" {
		dir = h.basePath
	}
	if dir == h.basePath {
		return rest, os.ErrPermission
	}
	pathNames := strings.Split(strings.TrimPrefix(dir, "/"), "/")
	sysUserVol, ok := h.suMaps[pathNames[1]]
	if !ok {
		return rest, os.ErrNotExist
	}

	if sysUserVol.client == nil {
		sftClient, conn, err := h.GetSftpClient(h.asset, sysUserVol.systemUser)
		if err != nil {
			return rest, os.ErrPermission
		}
		sysUserVol.homeDirPath, err = sftClient.Getwd()
		if err != nil {
			return rest, err
		}
		sysUserVol.client = sftClient
		sysUserVol.conn = conn

	}

	realPath := sysUserVol.ParsePath(dir)
	realFilenamePath := filepath.Join(realPath, filename)
	if !h.validatePermission(h.asset.ID, sysUserVol.systemUser.ID, model.UploadAction) {
		return rest, os.ErrPermission
	}

	fd, err := sysUserVol.client.Create(realFilenamePath)
	if err != nil {
		return rest, err
	}
	defer fd.Close()

	logData := &model.FTPLog{
		User:       fmt.Sprintf("%s (%s)", h.user.Name, h.user.Username),
		Hostname:   h.asset.Hostname,
		OrgID:      h.asset.OrgID,
		SystemUser: sysUserVol.systemUser.Name,
		RemoteAddr: h.Addr,
		Operate:    "Upload",
		Path:       realFilenamePath,
		DataStart:  common.CurrentUTCTime(),
		IsSuccess:  false,
	}
	defer h.CreateFTPLog(logData)
	_, err = io.Copy(fd, reader)
	if err != nil {
		return rest, err
	}
	logData.IsSuccess = true
	return h.Info(filepath.Join(dir, filename))
}

func (h *hostVolume) UploadChunk(cid int, dirPath, chunkName string, reader io.Reader) error {
	//chunkName format "filename.[NUMBER]_[TOTAL].part"
	var err error
	tmpDir := filepath.Join(h.localTmpPath, dirPath)
	err = common.EnsureDirExist(tmpDir)
	if err != nil {
		return err
	}
	chunkRealPath := fmt.Sprintf("%s_%d",
		filepath.Join(tmpDir, chunkName), cid)

	fd, err := os.Create(chunkRealPath)
	defer fd.Close()
	if err != nil {
		return err
	}
	_, err = io.Copy(fd, reader)
	return err
}

func (h *hostVolume) MergeChunk(cid, total int, dirPath, filename string) (elfinder.FileDir, error) {
	var rest elfinder.FileDir
	if h.basePath == dirPath {
		return rest, os.ErrPermission
	}
	pathNames := strings.Split(strings.TrimPrefix(dirPath, "/"), "/")
	sysUserVol, ok := h.suMaps[pathNames[1]]
	if !ok {
		return rest, os.ErrNotExist
	}

	if !h.validatePermission(h.asset.ID, sysUserVol.systemUser.ID, model.UploadAction) {
		for i := 0; i <= total; i++ {
			partPath := fmt.Sprintf("%s.%d_%d.part_%d",
				filepath.Join(h.localTmpPath, dirPath, filename), i, total, cid)
			_ = os.Remove(partPath)
		}
		return rest, os.ErrPermission
	}

	if sysUserVol.client == nil {
		sftClient, conn, err := h.GetSftpClient(h.asset, sysUserVol.systemUser)
		if err != nil {
			return rest, os.ErrPermission
		}
		sysUserVol.homeDirPath, err = sftClient.Getwd()
		if err != nil {
			return rest, err
		}
		sysUserVol.client = sftClient
		sysUserVol.conn = conn

	}

	realDirPath := sysUserVol.ParsePath(dirPath)
	filenamePath := filepath.Join(realDirPath, filename)
	logData := &model.FTPLog{
		User:       fmt.Sprintf("%s (%s)", h.user.Name, h.user.Username),
		Hostname:   h.asset.Hostname,
		OrgID:      h.asset.OrgID,
		SystemUser: sysUserVol.systemUser.Name,
		RemoteAddr: h.Addr,
		Operate:    "Upload",
		Path:       filenamePath,
		DataStart:  common.CurrentUTCTime(),
		IsSuccess:  false,
	}
	defer h.CreateFTPLog(logData)
	fd, err := sysUserVol.client.OpenFile(filenamePath, os.O_WRONLY|os.O_APPEND|os.O_CREATE|os.O_TRUNC)
	if err != nil {
		return rest, err
	}
	defer fd.Close()

	for i := 0; i <= total; i++ {
		partPath := fmt.Sprintf("%s.%d_%d.part_%d",
			filepath.Join(h.localTmpPath, dirPath, filename), i, total, cid)

		partFD, err := os.Open(partPath)
		if err != nil {
			logger.Debug(err)
			_ = os.Remove(partPath)
			continue
		}
		_, err = io.Copy(fd, partFD)
		if err != nil {
			return rest, os.ErrNotExist
		}
		_ = partFD.Close()
		_ = os.Remove(partPath)
	}
	logData.IsSuccess = true
	return h.Info(filepath.Join(dirPath, filename))
}

func (h *hostVolume) CompleteChunk(cid, total int, dirPath, filename string) bool {
	for i := 0; i <= total; i++ {
		partPath := fmt.Sprintf("%s.%d_%d.part_%d",
			filepath.Join(h.localTmpPath, dirPath, filename), i, total, cid)
		_, err := os.Stat(partPath)
		if err != nil {
			return false
		}
	}
	return true
}

func (h *hostVolume) MakeDir(dir, newDirname string) (elfinder.FileDir, error) {

	return h.Info(filepath.Join(dir, newDirname))
}

func (h *hostVolume) MakeFile(dir, newFilename string) (elfinder.FileDir, error) {

	return h.Info(filepath.Join(dir, newFilename))
}

func (h *hostVolume) Rename(oldNamePath, newName string) (elfinder.FileDir, error) {
	var rest elfinder.FileDir

	return h.Info(newFilePath)
}

func (h *hostVolume) Remove(path string) error {

	return err
}

func (h *hostVolume) Paste(dir, filename, suffix string, reader io.ReadCloser) (elfinder.FileDir, error) {

	return h.Info(realPath)
}

func (h *hostVolume) RootFileDir() elfinder.FileDir {
	var resFDir = elfinder.FileDir{}
	resFDir.Name = h.Name
	resFDir.Hash = hashPath(h.Uuid, h.basePath)
	resFDir.Mime = "directory"
	resFDir.Volumeid = h.Uuid
	resFDir.Dirs = 1
	resFDir.Read, resFDir.Write = 1, 1
	resFDir.Locked = 1
	return resFDir
}

func (h *hostVolume) GetSftpClient(asset *model.Asset, sysUser *model.SystemUser) (sftpClient *sftp.Client, sshClient *srvconn.SSHClient, err error) {
	sshClient, err = srvconn.NewClient(h.user, asset, sysUser, config.GetConf().SSHTimeout*time.Second)
	if err != nil {
		return
	}
	sftpClient, err = sftp.NewClient(sshClient.Client)
	if err != nil {
		return
	}
	return sftpClient, sshClient, nil
}

func (h *hostVolume) Close() {
}

func (h *hostVolume) CreateFTPLog(data *model.FTPLog) {
	for i := 0; i < 4; i++ {
		err := service.PushFTPLog(data)
		if err == nil {
			break
		}
		logger.Debugf("create FTP log err: %s", err.Error())
	}
}

func (h *hostVolume) validatePermission(aid, suid, operate string) bool {
	return true
}