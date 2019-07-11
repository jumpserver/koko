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

var (
	defaultHomeName = "Home"
)

func NewUserVolume(user *model.User, addr string) *UserVolume {
	rawID := fmt.Sprintf("'%s@%s", user.Username, addr)
	uVolume := &UserVolume{
		Uuid:     elfinder.GenerateID(rawID),
		Addr:     addr,
		user:     user,
		Name:     defaultHomeName,
		basePath: fmt.Sprintf("/%s", defaultHomeName),
	}
	uVolume.initial()
	return uVolume
}

type UserVolume struct {
	Uuid     string
	Addr     string
	Name     string
	basePath string
	user     *model.User
	assets   model.AssetList

	rootPath string //  tmp || home || ~
	hosts    map[string]*hostnameVolume

	localTmpPath string

	permCache map[string]bool
}

func (u *UserVolume) initial() {
	conf := config.GetConf()
	u.loadAssets()
	u.rootPath = conf.SftpRoot
	u.localTmpPath = filepath.Join(conf.RootPath, "data", "tmp")
	u.permCache = make(map[string]bool)
	_ = common.EnsureDirExist(u.localTmpPath)
	u.hosts = make(map[string]*hostnameVolume)
	for i, item := range u.assets {
		tmpDir := &hostnameVolume{
			VID:      u.ID(),
			homePath: u.basePath,
			hostPath: filepath.Join(u.basePath, item.Hostname),
			asset:    &u.assets[i],
			time:     time.Now().UTC(),
		}
		u.hosts[item.Hostname] = tmpDir
	}
}

func (u *UserVolume) loadAssets() {
	u.assets = service.GetUserAssets(u.user.ID, "1")
}

func (u *UserVolume) ID() string {
	return u.Uuid

}

func (u *UserVolume) Info(path string) (elfinder.FileDir, error) {
	var rest elfinder.FileDir
	if path == "" || path == "/" {
		path = u.basePath
	}
	if path == u.basePath {
		return u.RootFileDir(), nil
	}
	pathNames := strings.Split(strings.TrimPrefix(path, "/"), "/")
	hostVol, ok := u.hosts[pathNames[1]]
	if !ok {
		return rest, os.ErrNotExist
	}
	if hostVol.hostPath == path {
		return hostVol.info(), nil
	}

	if hostVol.suMaps == nil {
		hostVol.suMaps = make(map[string]*sysUserVolume)
		systemUsers := hostVol.asset.SystemUsers
		for i, sysUser := range systemUsers {
			hostVol.suMaps[sysUser.Name] = &sysUserVolume{
				VID:        u.ID(),
				hostpath:   hostVol.hostPath,
				suPath:     filepath.Join(hostVol.hostPath, sysUser.Name),
				systemUser: &systemUsers[i],
				rootPath:   u.rootPath,
			}
		}
	}

	sysUserVol, ok := hostVol.suMaps[pathNames[2]]
	if !ok {
		return rest, os.ErrNotExist
	}
	if path == sysUserVol.suPath {
		return sysUserVol.info(), nil
	}
	if !u.validatePermission(hostVol.asset.ID, sysUserVol.systemUser.ID, model.ConnectAction) {
		return rest, os.ErrPermission
	}

	if sysUserVol.client == nil {
		sftClient, conn, err := u.GetSftpClient(hostVol.asset, sysUserVol.systemUser)
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

	realPath := sysUserVol.ParsePath(path)
	dirname := filepath.Dir(path)
	fileInfos, err := sysUserVol.client.Stat(realPath)
	if err != nil {
		return rest, err
	}
	rest.Name = fileInfos.Name()
	rest.Hash = hashPath(u.ID(), path)
	rest.Phash = hashPath(u.ID(), dirname)
	rest.Size = fileInfos.Size()
	rest.Volumeid = u.ID()
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

func (u *UserVolume) List(path string) []elfinder.FileDir {
	var dirs []elfinder.FileDir
	if path == "" || path == "/" {
		path = u.basePath
	}
	if path == u.basePath {
		dirs = make([]elfinder.FileDir, 0, len(u.hosts))
		for _, item := range u.hosts {
			dirs = append(dirs, item.info())
		}
		return dirs
	}
	pathNames := strings.Split(strings.TrimPrefix(path, "/"), "/")
	hostVol, ok := u.hosts[pathNames[1]]
	if !ok {
		return dirs
	}

	if hostVol.suMaps == nil {
		hostVol.suMaps = make(map[string]*sysUserVolume)
		systemUsers := hostVol.asset.SystemUsers
		for i, sysUser := range systemUsers {
			hostVol.suMaps[sysUser.Name] = &sysUserVolume{
				VID:        u.ID(),
				hostpath:   hostVol.hostPath,
				suPath:     filepath.Join(hostVol.hostPath, sysUser.Name),
				systemUser: &systemUsers[i],
				rootPath:   u.rootPath,
			}
		}
	}
	if hostVol.hostPath == path {
		dirs = make([]elfinder.FileDir, 0, len(hostVol.suMaps))
		for _, item := range hostVol.suMaps {
			dirs = append(dirs, item.info())
		}
		return dirs
	}
	sysUserVol, ok := hostVol.suMaps[pathNames[2]]
	if !ok {
		return dirs
	}

	if sysUserVol.client == nil {
		sftClient, conn, err := u.GetSftpClient(hostVol.asset, sysUserVol.systemUser)
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
		fileDir, err := u.Info(filepath.Join(path, fInfo.Name()))
		if err != nil {
			continue
		}
		dirs = append(dirs, fileDir)
	}
	return dirs
}

func (u *UserVolume) Parents(path string, dep int) []elfinder.FileDir {
	relativepath := strings.TrimPrefix(strings.TrimPrefix(path, u.basePath), "/")
	relativePaths := strings.Split(relativepath, "/")
	dirs := make([]elfinder.FileDir, 0, len(relativePaths))

	for i := range relativePaths {
		realDirPath := filepath.Join(u.basePath, filepath.Join(relativePaths[:i]...))
		result, err := u.Info(realDirPath)
		if err != nil {
			continue
		}
		dirs = append(dirs, result)
		tmpDir := u.List(realDirPath)
		for j, item := range tmpDir {
			if item.Dirs == 1 {
				dirs = append(dirs, tmpDir[j])
			}
		}
	}
	return dirs
}

func (u *UserVolume) GetFile(path string) (reader io.ReadCloser, err error) {
	pathNames := strings.Split(strings.TrimPrefix(path, "/"), "/")
	hostVol, ok := u.hosts[pathNames[1]]
	if !ok {
		return nil, os.ErrNotExist
	}
	if hostVol.suMaps == nil {
		hostVol.suMaps = make(map[string]*sysUserVolume)
		systemUsers := hostVol.asset.SystemUsers
		for i, sysUser := range systemUsers {
			hostVol.suMaps[sysUser.Name] = &sysUserVolume{
				VID:        u.ID(),
				hostpath:   hostVol.hostPath,
				suPath:     filepath.Join(hostVol.hostPath, sysUser.Name),
				systemUser: &systemUsers[i],
				rootPath:   u.rootPath,
			}
		}
	}
	sysUserVol, ok := hostVol.suMaps[pathNames[2]]
	if !ok {
		return nil, os.ErrNotExist
	}

	if !u.validatePermission(hostVol.asset.ID, sysUserVol.systemUser.ID, model.DownloadAction) {
		return nil, os.ErrPermission
	}

	if sysUserVol.client == nil {
		sftClient, conn, err := u.GetSftpClient(hostVol.asset, sysUserVol.systemUser)
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
		User:       fmt.Sprintf("%s (%s)", u.user.Name, u.user.Username),
		Hostname:   hostVol.asset.Hostname,
		OrgID:      hostVol.asset.OrgID,
		SystemUser: sysUserVol.systemUser.Name,
		RemoteAddr: u.Addr,
		Operate:    "Download",
		Path:       realPath,
		DataStart:  common.CurrentUTCTime(),
		IsSuccess:  false,
	}
	defer u.CreateFTPLog(logData)
	reader, err = sysUserVol.client.Open(realPath)
	if err != nil {
		return
	}
	logData.IsSuccess = true
	return
}

func (u *UserVolume) UploadFile(dir, filename string, reader io.Reader) (elfinder.FileDir, error) {
	var rest elfinder.FileDir
	var err error
	if dir == "" || dir == "/" {
		dir = u.basePath
	}
	if dir == u.basePath {
		return rest, os.ErrPermission
	}
	pathNames := strings.Split(strings.TrimPrefix(dir, "/"), "/")
	hostVol, ok := u.hosts[pathNames[1]]
	if !ok {
		return rest, os.ErrNotExist
	}
	if hostVol.hostPath == dir {
		return rest, os.ErrPermission
	}
	if hostVol.suMaps == nil {
		hostVol.suMaps = make(map[string]*sysUserVolume)
		systemUsers := hostVol.asset.SystemUsers
		for i, sysUser := range systemUsers {
			hostVol.suMaps[sysUser.Name] = &sysUserVolume{
				VID:        u.ID(),
				hostpath:   hostVol.hostPath,
				suPath:     filepath.Join(hostVol.hostPath, sysUser.Name),
				systemUser: &systemUsers[i],
				rootPath:   u.rootPath,
			}
		}
	}

	sysUserVol, ok := hostVol.suMaps[pathNames[2]]
	if !ok {
		return rest, os.ErrNotExist
	}

	if sysUserVol.client == nil {
		sftClient, conn, err := u.GetSftpClient(hostVol.asset, sysUserVol.systemUser)
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
	if !u.validatePermission(hostVol.asset.ID, sysUserVol.systemUser.ID, model.UploadAction) {
		return rest, os.ErrPermission
	}

	fd, err := sysUserVol.client.Create(realFilenamePath)
	if err != nil {
		return rest, err
	}
	defer fd.Close()

	logData := &model.FTPLog{
		User:       fmt.Sprintf("%s (%s)", u.user.Name, u.user.Username),
		Hostname:   hostVol.asset.Hostname,
		OrgID:      hostVol.asset.OrgID,
		SystemUser: sysUserVol.systemUser.Name,
		RemoteAddr: u.Addr,
		Operate:    "Upload",
		Path:       realFilenamePath,
		DataStart:  common.CurrentUTCTime(),
		IsSuccess:  false,
	}
	defer u.CreateFTPLog(logData)
	_, err = io.Copy(fd, reader)
	if err != nil {
		return rest, err
	}
	logData.IsSuccess = true
	return u.Info(filepath.Join(dir, filename))
}

func (u *UserVolume) UploadChunk(cid int, dirPath, chunkName string, reader io.Reader) error {
	//chunkName format "filename.[NUMBER]_[TOTAL].part"
	var err error
	tmpDir := filepath.Join(u.localTmpPath, dirPath)
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

func (u *UserVolume) MergeChunk(cid, total int, dirPath, filename string) (elfinder.FileDir, error) {
	var rest elfinder.FileDir
	if u.basePath == dirPath {
		return rest, os.ErrPermission
	}
	pathNames := strings.Split(strings.TrimPrefix(dirPath, "/"), "/")
	hostVol, ok := u.hosts[pathNames[1]]
	if !ok {
		return rest, os.ErrNotExist
	}
	if hostVol.hostPath == dirPath {
		return rest, os.ErrPermission
	}
	if hostVol.suMaps == nil {
		hostVol.suMaps = make(map[string]*sysUserVolume)
		systemUsers := hostVol.asset.SystemUsers
		for i, sysUser := range systemUsers {
			hostVol.suMaps[sysUser.Name] = &sysUserVolume{
				VID:        u.ID(),
				hostpath:   hostVol.hostPath,
				suPath:     filepath.Join(hostVol.hostPath, sysUser.Name),
				systemUser: &systemUsers[i],
				rootPath:   u.rootPath,
			}
		}
	}

	sysUserVol, ok := hostVol.suMaps[pathNames[2]]
	if !ok {
		return rest, os.ErrNotExist
	}

	if !u.validatePermission(hostVol.asset.ID, sysUserVol.systemUser.ID, model.UploadAction) {
		for i := 0; i <= total; i++ {
			partPath := fmt.Sprintf("%s.%d_%d.part_%d",
				filepath.Join(u.localTmpPath, dirPath, filename), i, total, cid)
			_ = os.Remove(partPath)
		}
		return rest, os.ErrPermission
	}

	if sysUserVol.client == nil {
		sftClient, conn, err := u.GetSftpClient(hostVol.asset, sysUserVol.systemUser)
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
		User:       fmt.Sprintf("%s (%s)", u.user.Name, u.user.Username),
		Hostname:   hostVol.asset.Hostname,
		OrgID:      hostVol.asset.OrgID,
		SystemUser: sysUserVol.systemUser.Name,
		RemoteAddr: u.Addr,
		Operate:    "Upload",
		Path:       filenamePath,
		DataStart:  common.CurrentUTCTime(),
		IsSuccess:  false,
	}
	defer u.CreateFTPLog(logData)
	fd, err := sysUserVol.client.OpenFile(filenamePath, os.O_WRONLY|os.O_APPEND|os.O_CREATE|os.O_TRUNC)
	if err != nil {
		return rest, err
	}
	defer fd.Close()

	for i := 0; i <= total; i++ {
		partPath := fmt.Sprintf("%s.%d_%d.part_%d",
			filepath.Join(u.localTmpPath, dirPath, filename), i, total, cid)

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
	return u.Info(filepath.Join(dirPath, filename))
}

func (u *UserVolume) CompleteChunk(cid, total int, dirPath, filename string) bool {
	for i := 0; i <= total; i++ {
		partPath := fmt.Sprintf("%s.%d_%d.part_%d",
			filepath.Join(u.localTmpPath, dirPath, filename), i, total, cid)
		_, err := os.Stat(partPath)
		if err != nil {
			return false
		}
	}
	return true
}

func (u *UserVolume) MakeDir(dir, newDirname string) (elfinder.FileDir, error) {
	var rest elfinder.FileDir
	if dir == "" || dir == "/" {
		dir = u.basePath
	}
	if dir == u.basePath {
		return rest, os.ErrPermission
	}
	pathNames := strings.Split(strings.TrimPrefix(dir, "/"), "/")
	hostVol, ok := u.hosts[pathNames[1]]
	if !ok {
		return rest, os.ErrNotExist
	}
	if hostVol.hostPath == dir {
		return rest, os.ErrPermission
	}
	if hostVol.suMaps == nil {
		hostVol.suMaps = make(map[string]*sysUserVolume)
		systemUsers := hostVol.asset.SystemUsers
		for i, sysUser := range systemUsers {
			hostVol.suMaps[sysUser.Name] = &sysUserVolume{
				VID:        u.ID(),
				hostpath:   hostVol.hostPath,
				suPath:     filepath.Join(hostVol.hostPath, sysUser.Name),
				systemUser: &systemUsers[i],
				rootPath:   u.rootPath,
			}
		}
	}
	sysUserVol, ok := hostVol.suMaps[pathNames[2]]
	if !ok {
		return rest, os.ErrNotExist
	}

	if !u.validatePermission(hostVol.asset.ID, sysUserVol.systemUser.ID, model.ConnectAction) {
		return rest, os.ErrPermission
	}

	if sysUserVol.client == nil {
		sftClient, conn, err := u.GetSftpClient(hostVol.asset, sysUserVol.systemUser)
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
	realDirPath := filepath.Join(realPath, newDirname)
	err := sysUserVol.client.MkdirAll(realDirPath)
	logData := &model.FTPLog{
		User:       fmt.Sprintf("%s (%s)", u.user.Name, u.user.Username),
		Hostname:   hostVol.asset.Hostname,
		OrgID:      hostVol.asset.OrgID,
		SystemUser: sysUserVol.systemUser.Name,
		RemoteAddr: u.Addr,
		Operate:    "Mkdir",
		Path:       realDirPath,
		DataStart:  common.CurrentUTCTime(),
		IsSuccess:  false,
	}
	defer u.CreateFTPLog(logData)
	if err != nil {
		return rest, err
	}
	logData.IsSuccess = true
	return u.Info(filepath.Join(dir, newDirname))
}

func (u *UserVolume) MakeFile(dir, newFilename string) (elfinder.FileDir, error) {
	var rest elfinder.FileDir
	if dir == "" || dir == "/" {
		dir = u.basePath
	}
	if dir == u.basePath {
		return rest, os.ErrPermission
	}
	pathNames := strings.Split(strings.TrimPrefix(dir, "/"), "/")
	hostVol, ok := u.hosts[pathNames[1]]
	if !ok {
		return rest, os.ErrNotExist
	}
	if hostVol.hostPath == dir {
		return rest, os.ErrPermission
	}
	if hostVol.suMaps == nil {
		hostVol.suMaps = make(map[string]*sysUserVolume)
		systemUsers := hostVol.asset.SystemUsers
		for i, sysUser := range systemUsers {
			hostVol.suMaps[sysUser.Name] = &sysUserVolume{
				VID:        u.ID(),
				hostpath:   hostVol.hostPath,
				suPath:     filepath.Join(hostVol.hostPath, sysUser.Name),
				systemUser: &systemUsers[i],
				rootPath:   u.rootPath,
			}
		}
	}
	sysUserVol, ok := hostVol.suMaps[pathNames[2]]
	if !ok {
		return rest, os.ErrNotExist
	}

	if !u.validatePermission(hostVol.asset.ID, sysUserVol.systemUser.ID, model.ConnectAction) {
		return rest, os.ErrPermission
	}

	if sysUserVol.client == nil {
		sftClient, conn, err := u.GetSftpClient(hostVol.asset, sysUserVol.systemUser)
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
	realFilePath := filepath.Join(realPath, newFilename)
	_, err := sysUserVol.client.Create(realFilePath)
	logData := &model.FTPLog{
		User:       fmt.Sprintf("%s (%s)", u.user.Name, u.user.Username),
		Hostname:   hostVol.asset.Hostname,
		OrgID:      hostVol.asset.OrgID,
		SystemUser: sysUserVol.systemUser.Name,
		RemoteAddr: u.Addr,
		Operate:    "Append",
		Path:       realFilePath,
		DataStart:  common.CurrentUTCTime(),
		IsSuccess:  false,
	}
	defer u.CreateFTPLog(logData)
	if err != nil {
		return rest, err
	}
	logData.IsSuccess = true
	return u.Info(filepath.Join(dir, newFilename))
}

func (u *UserVolume) Rename(oldNamePath, newName string) (elfinder.FileDir, error) {
	var rest elfinder.FileDir
	pathNames := strings.Split(strings.TrimPrefix(oldNamePath, "/"), "/")
	hostVol, ok := u.hosts[pathNames[1]]
	if !ok {
		return rest, os.ErrNotExist
	}
	if hostVol.suMaps == nil {
		hostVol.suMaps = make(map[string]*sysUserVolume)
		systemUsers := hostVol.asset.SystemUsers
		for i, sysUser := range systemUsers {
			hostVol.suMaps[sysUser.Name] = &sysUserVolume{
				VID:        u.ID(),
				hostpath:   hostVol.hostPath,
				suPath:     filepath.Join(hostVol.hostPath, sysUser.Name),
				systemUser: &systemUsers[i],
				rootPath:   u.rootPath,
			}
		}
	}
	sysUserVol, ok := hostVol.suMaps[pathNames[2]]
	if !ok {
		return rest, os.ErrNotExist
	}
	if sysUserVol.suPath == oldNamePath {
		return rest, os.ErrPermission
	}

	if !u.validatePermission(hostVol.asset.ID, sysUserVol.systemUser.ID, model.ConnectAction) {
		return rest, os.ErrPermission
	}

	if sysUserVol.client == nil {
		sftClient, conn, err := u.GetSftpClient(hostVol.asset, sysUserVol.systemUser)
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

	realPath := sysUserVol.ParsePath(oldNamePath)
	dirpath := filepath.Dir(realPath)
	newFilePath := filepath.Join(dirpath, newName)

	err := sysUserVol.client.Rename(oldNamePath, newFilePath)
	logData := &model.FTPLog{
		User:       fmt.Sprintf("%s (%s)", u.user.Name, u.user.Username),
		Hostname:   hostVol.asset.Hostname,
		OrgID:      hostVol.asset.OrgID,
		SystemUser: sysUserVol.systemUser.Name,
		RemoteAddr: u.Addr,
		Operate:    "Rename",
		Path:       fmt.Sprintf("%s=>%s", oldNamePath, newFilePath),
		DataStart:  common.CurrentUTCTime(),
		IsSuccess:  false,
	}
	defer u.CreateFTPLog(logData)
	if err != nil {
		return rest, err
	}
	logData.IsSuccess = true
	return u.Info(newFilePath)
}

func (u *UserVolume) Remove(path string) error {
	if path == "" || path == "/" {
		path = u.basePath
	}
	if path == u.basePath {
		return os.ErrPermission
	}
	pathNames := strings.Split(strings.TrimPrefix(path, "/"), "/")
	hostVol, ok := u.hosts[pathNames[1]]
	if !ok {
		return os.ErrNotExist
	}
	if hostVol.suMaps == nil {
		hostVol.suMaps = make(map[string]*sysUserVolume)
		systemUsers := hostVol.asset.SystemUsers
		for i, sysUser := range systemUsers {
			hostVol.suMaps[sysUser.Name] = &sysUserVolume{
				VID:        u.ID(),
				hostpath:   hostVol.hostPath,
				suPath:     filepath.Join(hostVol.hostPath, sysUser.Name),
				systemUser: &systemUsers[i],
				rootPath:   u.rootPath,
			}
		}
	}
	sysUserVol, ok := hostVol.suMaps[pathNames[2]]
	if !ok {
		return os.ErrNotExist
	}
	if sysUserVol.suPath == path {
		return os.ErrPermission
	}

	if !u.validatePermission(hostVol.asset.ID, sysUserVol.systemUser.ID, model.ConnectAction) {
		return os.ErrPermission
	}

	if sysUserVol.client == nil {
		sftClient, conn, err := u.GetSftpClient(hostVol.asset, sysUserVol.systemUser)
		if err != nil {
			return os.ErrPermission
		}
		sysUserVol.homeDirPath, err = sftClient.Getwd()
		if err != nil {
			return err
		}
		sysUserVol.client = sftClient
		sysUserVol.conn = conn

	}

	realPath := sysUserVol.ParsePath(path)
	logData := &model.FTPLog{
		User:       fmt.Sprintf("%s (%s)", u.user.Name, u.user.Username),
		Hostname:   hostVol.asset.Hostname,
		OrgID:      hostVol.asset.OrgID,
		SystemUser: sysUserVol.systemUser.Name,
		RemoteAddr: u.Addr,
		Operate:    "Delete",
		Path:       realPath,
		DataStart:  common.CurrentUTCTime(),
		IsSuccess:  false,
	}
	defer u.CreateFTPLog(logData)
	err := sysUserVol.client.Remove(realPath)
	if err == nil {
		logData.IsSuccess = true
	}
	return err
}

func (u *UserVolume) Paste(dir, filename, suffix string, reader io.ReadCloser) (elfinder.FileDir, error) {
	var rest elfinder.FileDir
	if dir == "" || dir == "/" {
		dir = u.basePath
	}
	if dir == u.basePath {
		return rest, os.ErrPermission
	}
	pathNames := strings.Split(strings.TrimPrefix(dir, "/"), "/")
	hostVol, ok := u.hosts[pathNames[1]]
	if !ok {
		return rest, os.ErrNotExist
	}
	if hostVol.hostPath == dir {
		return rest, os.ErrPermission
	}
	if hostVol.suMaps == nil {
		hostVol.suMaps = make(map[string]*sysUserVolume)
		systemUsers := hostVol.asset.SystemUsers
		for i, sysUser := range systemUsers {
			hostVol.suMaps[sysUser.Name] = &sysUserVolume{
				VID:        u.ID(),
				hostpath:   hostVol.hostPath,
				suPath:     filepath.Join(hostVol.hostPath, sysUser.Name),
				systemUser: &systemUsers[i],
				rootPath:   u.rootPath,
			}
		}
	}
	sysUserVol, ok := hostVol.suMaps[pathNames[2]]
	if !ok {
		return rest, os.ErrNotExist
	}
	if !u.validatePermission(hostVol.asset.ID, sysUserVol.systemUser.ID, model.UploadAction) {
		return rest, os.ErrPermission
	}

	if sysUserVol.client == nil {
		sftClient, conn, err := u.GetSftpClient(hostVol.asset, sysUserVol.systemUser)
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
	realFilePath := filepath.Join(realPath, filename)
	_, err := sysUserVol.client.Stat(realFilePath)
	if err != nil {
		realFilePath += suffix
	}
	logData := &model.FTPLog{
		User:       fmt.Sprintf("%s (%s)", u.user.Name, u.user.Username),
		Hostname:   hostVol.asset.Hostname,
		OrgID:      hostVol.asset.OrgID,
		SystemUser: sysUserVol.systemUser.Name,
		RemoteAddr: u.Addr,
		Operate:    "Append",
		Path:       realFilePath,
		DataStart:  common.CurrentUTCTime(),
		IsSuccess:  false,
	}
	defer u.CreateFTPLog(logData)
	fd, err := sysUserVol.client.OpenFile(realPath, os.O_RDWR|os.O_CREATE)
	if err != nil {
		return rest, err
	}
	defer fd.Close()
	_, err = io.Copy(fd, reader)
	if err != nil {
		return rest, err
	}
	logData.IsSuccess = true
	return u.Info(realPath)
}

func (u *UserVolume) RootFileDir() elfinder.FileDir {
	var resFDir = elfinder.FileDir{}
	resFDir.Name = u.Name
	resFDir.Hash = hashPath(u.Uuid, u.basePath)
	resFDir.Mime = "directory"
	resFDir.Volumeid = u.Uuid
	resFDir.Dirs = 1
	resFDir.Read, resFDir.Write = 1, 1
	resFDir.Locked = 1
	return resFDir
}

func (u *UserVolume) GetSftpClient(asset *model.Asset, sysUser *model.SystemUser) (sftpClient *sftp.Client, sshClient *srvconn.SSHClient, err error) {
	sshClient, err = srvconn.NewClient(u.user, asset, sysUser, config.GetConf().SSHTimeout*time.Second)
	if err != nil {
		return
	}
	sftpClient, err = sftp.NewClient(sshClient.Client)
	if err != nil {
		return
	}
	return sftpClient, sshClient, nil
}

func (u *UserVolume) Close() {
	for _, host := range u.hosts {
		if host.suMaps == nil {
			continue
		}
		for _, su := range host.suMaps {
			su.Close()
		}
	}
}

func (u *UserVolume) CreateFTPLog(data *model.FTPLog) {
	for i := 0; i < 4; i++ {
		err := service.PushFTPLog(data)
		if err == nil {
			break
		}
		logger.Debugf("create FTP log err: %s", err.Error())
		time.Sleep(500 * time.Millisecond)
	}
}

func (u *UserVolume) validatePermission(aid, suid, operate string) bool {
	permKey := fmt.Sprintf("%s_%s_%s", aid, suid, operate)
	permission, ok := u.permCache[permKey]
	if ok {
		return permission
	}
	permission = service.ValidateUserAssetPermission(
		u.user.ID, aid, suid, operate,
	)
	u.permCache[permKey] = permission
	return permission
}

type hostnameVolume struct {
	VID      string
	homePath string
	hostPath string // /home/hostname/
	time     time.Time
	asset    *model.Asset
	suMaps   map[string]*sysUserVolume
}

func (h *hostnameVolume) info() elfinder.FileDir {
	var resFDir = elfinder.FileDir{}
	resFDir.Name = h.asset.Hostname
	resFDir.Hash = hashPath(h.VID, h.hostPath)
	resFDir.Phash = hashPath(h.VID, h.homePath)
	resFDir.Mime = "directory"
	resFDir.Volumeid = h.VID
	resFDir.Dirs = 1
	resFDir.Read, resFDir.Write = 1, 1
	return resFDir
}

type sysUserVolume struct {
	VID        string
	hostpath   string
	suPath     string
	rootPath   string
	systemUser *model.SystemUser

	homeDirPath string
	client      *sftp.Client
	conn        *srvconn.SSHClient
}

func (su *sysUserVolume) info() elfinder.FileDir {
	var resFDir = elfinder.FileDir{}
	resFDir.Name = su.systemUser.Name
	resFDir.Hash = hashPath(su.VID, su.suPath)
	resFDir.Phash = hashPath(su.VID, su.hostpath)
	resFDir.Mime = "directory"
	resFDir.Volumeid = su.VID
	resFDir.Dirs = 1
	resFDir.Read, resFDir.Write = 1, 1
	return resFDir
}

func (su *sysUserVolume) ParsePath(path string) string {
	var realPath string
	switch strings.ToLower(su.rootPath) {
	case "home", "~", "":
		realPath = strings.ReplaceAll(path, su.suPath, su.homeDirPath)
	default:
		realPath = strings.ReplaceAll(path, su.suPath, su.rootPath)
	}
	logger.Debug("real path: ", realPath)
	return realPath
}

func (su *sysUserVolume) Close() {
	if su.client != nil {
		_ = su.client.Close()
	}
	srvconn.RecycleClient(su.conn)
}

func hashPath(id, path string) string {
	return elfinder.CreateHash(id, path)
}
