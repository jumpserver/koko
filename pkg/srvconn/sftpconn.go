package srvconn

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/pkg/sftp"

	"github.com/jumpserver/koko/pkg/common"
	"github.com/jumpserver/koko/pkg/config"
	"github.com/jumpserver/koko/pkg/logger"
	"github.com/jumpserver/koko/pkg/model"
	"github.com/jumpserver/koko/pkg/service"
)

func NewUserSFTP(user *model.User, addr string, assets ...model.Asset) *UserSftp {
	u := UserSftp{
		User: user, Addr: addr,
	}
	u.initial(assets)
	return &u
}

type UserSftp struct {
	User        *model.User
	Addr        string
	RootPath    string
	hosts       map[string]*HostnameDir // key hostname or hostname.orgName
	sftpClients map[string]*SftpConn    //  key %s@%s suName hostName

	LogChan chan *model.FTPLog
}

func (u *UserSftp) initial(assets []model.Asset) {

	u.RootPath = config.GetConf().SftpRoot
	u.hosts = make(map[string]*HostnameDir)
	u.sftpClients = make(map[string]*SftpConn)
	u.LogChan = make(chan *model.FTPLog, 10)
	for i := 0; i < len(assets); i++ {
		if !assets[i].IsSupportProtocol("ssh") {
			continue
		}
		key := assets[i].Hostname
		if assets[i].OrgID != "" {
			key = fmt.Sprintf("%s.%s", assets[i].Hostname, assets[i].OrgName)
		}
		u.hosts[key] = NewHostnameDir(&assets[i])
	}

	go u.LoopPushFTPLog()
}

func (u *UserSftp) ReadDir(path string) (res []os.FileInfo, err error) {
	req := u.ParsePath(path)
	if req.host == "" {
		return u.RootDirInfo()
	}

	host, ok := u.hosts[req.host]
	if !ok {
		return res, sftp.ErrSshFxNoSuchFile
	}

	if req.su == "" {
		for _, su := range host.GetSystemUsers() {
			res = append(res, NewFakeFile(su.Name, true))
		}
		return
	}
	su, ok := host.suMaps[req.su]
	if !ok {
		return res, sftp.ErrSshFxNoSuchFile
	}
	if !u.validatePermission(su, model.ConnectAction) {
		return res, sftp.ErrSshFxPermissionDenied
	}

	conn, realPath := u.GetSFTPAndRealPath(req)
	if conn == nil {
		return res, sftp.ErrSshFxPermissionDenied
	}
	logger.Debug("intersftp read dir real path: ", realPath)
	res, err = conn.client.ReadDir(realPath)
	return res, err
}

func (u *UserSftp) Stat(path string) (res os.FileInfo, err error) {
	req := u.ParsePath(path)
	if req.host == "" {
		return u.Info()
	}
	host, ok := u.hosts[req.host]
	if !ok {
		return res, sftp.ErrSshFxNoSuchFile
	}

	if req.su == "" {
		res = NewFakeFile(req.host, true)
		return
	}
	su, ok := host.suMaps[req.su]
	if !ok {
		return res, sftp.ErrSshFxNoSuchFile
	}
	if !u.validatePermission(su, model.ConnectAction) {
		return res, sftp.ErrSshFxPermissionDenied
	}

	conn, realPath := u.GetSFTPAndRealPath(req)
	if conn == nil {
		return res, sftp.ErrSshFxPermissionDenied
	}
	return conn.client.Stat(realPath)
}

func (u *UserSftp) ReadLink(path string) (res string, err error) {
	req := u.ParsePath(path)
	if req.host == "" {
		return res, sftp.ErrSshFxPermissionDenied
	}
	host, ok := u.hosts[req.host]
	if !ok {
		return res, sftp.ErrSshFxPermissionDenied
	}

	if req.su == "" {
		return res, sftp.ErrSshFxPermissionDenied
	}

	su, ok := host.suMaps[req.su]
	if !ok {
		return res, sftp.ErrSshFxNoSuchFile
	}
	if !u.validatePermission(su, model.ConnectAction) {
		return res, sftp.ErrSshFxPermissionDenied
	}
	conn, realPath := u.GetSFTPAndRealPath(req)
	if conn == nil {
		return res, sftp.ErrSshFxPermissionDenied
	}
	return conn.client.ReadLink(realPath)
}

func (u *UserSftp) RemoveDirectory(path string) error {
	req := u.ParsePath(path)
	if req.host == "" {
		return sftp.ErrSshFxPermissionDenied
	}
	host, ok := u.hosts[req.host]
	if !ok {
		return sftp.ErrSshFxPermissionDenied
	}
	if req.su == "" {
		return sftp.ErrSshFxPermissionDenied
	}
	su, ok := host.suMaps[req.su]
	if !ok {
		return sftp.ErrSshFxNoSuchFile
	}

	if !u.validatePermission(su, model.UploadAction) {
		return sftp.ErrSshFxPermissionDenied
	}

	conn, realPath := u.GetSFTPAndRealPath(req)
	if conn == nil {
		return sftp.ErrSshFxPermissionDenied
	}
	err := u.removeDirectoryAll(conn.client, realPath)
	filename := realPath
	isSucess := false
	operate := model.OperateRemoveDir
	if err == nil {
		isSucess = true
	}
	u.CreateFTPLog(host.asset, su, operate, filename, isSucess)
	return err
}

func (u *UserSftp) removeDirectoryAll(conn *sftp.Client, path string) error {
	var err error
	var files []os.FileInfo
	files, err = conn.ReadDir(path)
	if err != nil {
		return err
	}
	for _, item := range files {
		realPath := filepath.Join(path, item.Name())

		if item.IsDir() {
			err = u.removeDirectoryAll(conn, realPath)
			if err != nil {
				return err
			}
			continue
		}
		err = conn.Remove(realPath)
		if err != nil {
			return err
		}
	}
	return conn.RemoveDirectory(path)
}

func (u *UserSftp) Remove(path string) error {
	req := u.ParsePath(path)
	if req.host == "" {
		return sftp.ErrSshFxPermissionDenied
	}
	host, ok := u.hosts[req.host]
	if !ok {
		return sftp.ErrSshFxPermissionDenied
	}
	if req.su == "" {
		return sftp.ErrSshFxPermissionDenied
	}

	su, ok := host.suMaps[req.su]
	if !ok {
		return sftp.ErrSshFxNoSuchFile
	}
	if !u.validatePermission(su, model.UploadAction) {
		return sftp.ErrSshFxPermissionDenied
	}

	conn, realPath := u.GetSFTPAndRealPath(req)
	if conn == nil {
		return sftp.ErrSshFxPermissionDenied
	}
	err := conn.client.Remove(realPath)
	filename := realPath
	isSucess := false
	operate := model.OperateDelete
	if err == nil {
		isSucess = true
	}
	u.CreateFTPLog(host.asset, su, operate, filename, isSucess)
	return err
}

func (u *UserSftp) MkdirAll(path string) error {
	req := u.ParsePath(path)
	if req.host == "" {
		return sftp.ErrSshFxPermissionDenied
	}
	host, ok := u.hosts[req.host]
	if !ok {
		return sftp.ErrSshFxPermissionDenied
	}
	if req.su == "" {
		return sftp.ErrSshFxPermissionDenied
	}
	su, ok := host.suMaps[req.su]
	if !ok {
		return sftp.ErrSshFxNoSuchFile
	}
	if !u.validatePermission(su, model.UploadAction) {
		return sftp.ErrSshFxPermissionDenied
	}

	conn, realPath := u.GetSFTPAndRealPath(req)
	if conn == nil {
		return sftp.ErrSshFxPermissionDenied
	}
	err := conn.client.MkdirAll(realPath)

	filename := realPath
	isSucess := false
	operate := model.OperateMkdir
	if err == nil {
		isSucess = true
	}
	u.CreateFTPLog(host.asset, su, operate, filename, isSucess)
	return err
}

func (u *UserSftp) Rename(oldNamePath, newNamePath string) error {
	req1 := u.ParsePath(oldNamePath)
	req2 := u.ParsePath(newNamePath)
	if req1.host == "" || req2.host == "" || req1.su == "" || req2.su == "" {
		return sftp.ErrSshFxPermissionDenied
	} else if req1.host != req2.host || req1.su != req2.su {
		return sftp.ErrSshFxPermissionDenied
	}
	host, ok := u.hosts[req1.host]
	if !ok {
		return sftp.ErrSshFxPermissionDenied
	}
	su, ok := host.suMaps[req1.su]
	if !ok {
		return sftp.ErrSshFxNoSuchFile
	}
	if !u.validatePermission(su, model.UploadAction) {
		return sftp.ErrSshFxPermissionDenied
	}

	conn1, oldRealPath := u.GetSFTPAndRealPath(req1)
	conn2, newRealPath := u.GetSFTPAndRealPath(req2)
	if conn1 != conn2 {
		return sftp.ErrSshFxOpUnsupported
	}

	err := conn1.client.Rename(oldRealPath, newRealPath)
	filename := fmt.Sprintf("%s=>%s", oldRealPath, newRealPath)
	isSucess := false
	operate := model.OperateRename
	if err == nil {
		isSucess = true
	}
	u.CreateFTPLog(host.asset, su, operate, filename, isSucess)
	return err
}

func (u *UserSftp) Symlink(oldNamePath, newNamePath string) error {
	req1 := u.ParsePath(oldNamePath)
	req2 := u.ParsePath(newNamePath)
	if req1.host == "" || req2.host == "" || req1.su == "" || req2.su == "" {
		return sftp.ErrSshFxPermissionDenied
	} else if req1.host != req2.host || req1.su != req2.su {
		return sftp.ErrSshFxPermissionDenied
	}
	host, ok := u.hosts[req1.host]
	if !ok {
		return sftp.ErrSshFxPermissionDenied
	}
	su, ok := host.suMaps[req1.su]
	if !ok {
		return sftp.ErrSshFxNoSuchFile
	}
	if !u.validatePermission(su, model.UploadAction) {
		return sftp.ErrSshFxPermissionDenied
	}

	conn1, oldRealPath := u.GetSFTPAndRealPath(req1)
	conn2, newRealPath := u.GetSFTPAndRealPath(req2)
	if conn1 != conn2 {
		return sftp.ErrSshFxOpUnsupported
	}
	err := conn1.client.Symlink(oldRealPath, newRealPath)

	filename := fmt.Sprintf("%s=>%s", oldRealPath, newRealPath)
	isSucess := false
	operate := model.OperateSymlink
	if err == nil {
		isSucess = true
	}
	u.CreateFTPLog(host.asset, su, operate, filename, isSucess)
	return err
}

func (u *UserSftp) Create(path string) (*sftp.File, error) {
	req := u.ParsePath(path)
	if req.host == "" {
		return nil, sftp.ErrSshFxPermissionDenied
	}
	host, ok := u.hosts[req.host]
	if !ok {
		return nil, sftp.ErrSshFxPermissionDenied
	}
	if req.su == "" {
		return nil, sftp.ErrSshFxPermissionDenied
	}

	su, ok := host.suMaps[req.su]
	if !ok {
		return nil, sftp.ErrSshFxNoSuchFile
	}
	if !u.validatePermission(su, model.UploadAction) {
		return nil, sftp.ErrSshFxPermissionDenied
	}

	conn, realPath := u.GetSFTPAndRealPath(req)
	if conn == nil {
		return nil, sftp.ErrSshFxPermissionDenied
	}
	sf, err := conn.client.Create(realPath)
	filename := realPath
	isSucess := false
	operate := model.OperateUpload
	if err == nil {
		isSucess = true
	}
	u.CreateFTPLog(host.asset, su, operate, filename, isSucess)
	return sf, err
}

func (u *UserSftp) Open(path string) (*sftp.File, error) {
	req := u.ParsePath(path)
	if req.host == "" {
		return nil, sftp.ErrSshFxPermissionDenied
	}
	host, ok := u.hosts[req.host]
	if !ok {
		return nil, sftp.ErrSshFxPermissionDenied
	}
	if req.su == "" {
		return nil, sftp.ErrSshFxPermissionDenied
	}
	su, ok := host.suMaps[req.su]
	if !ok {
		return nil, sftp.ErrSshFxNoSuchFile
	}
	if !u.validatePermission(su, model.DownloadAction) {
		return nil, sftp.ErrSshFxPermissionDenied
	}
	conn, realPath := u.GetSFTPAndRealPath(req)
	if conn == nil {
		return nil, sftp.ErrSshFxPermissionDenied
	}
	sf, err := conn.client.Open(realPath)
	filename := realPath
	isSucess := false
	operate := model.OperateDownaload
	if err == nil {
		isSucess = true
	}
	u.CreateFTPLog(host.asset, su, operate, filename, isSucess)
	return sf, err
}

func (u *UserSftp) Info() (os.FileInfo, error) {
	return NewFakeFile("/", true), nil
}

func (u *UserSftp) RootDirInfo() ([]os.FileInfo, error) {
	hostDirs := make([]os.FileInfo, 0, len(u.hosts))
	for key := range u.hosts {
		hostDirs = append(hostDirs, NewFakeFile(key, true))
	}
	sort.Sort(FileInfoList(hostDirs))
	return hostDirs, nil
}

func (u *UserSftp) ParsePath(path string) (req requestMessage) {
	data := strings.Split(strings.TrimPrefix(path, "/"), "/")
	if len(data) == 0 {
		return
	}
	host, pathArray := data[0], data[1:]
	req.host = host
	if suName, unique := u.HostHasUniqueSu(host); unique {
		req.suUnique = true
		req.su = suName
	} else {
		if len(pathArray) == 0 {
			req.su = ""
		} else {
			req.su, pathArray = pathArray[0], pathArray[1:]
		}
	}
	req.dpath = strings.Join(pathArray, "/")
	return
}

func (u *UserSftp) GetSFTPAndRealPath(req requestMessage) (conn *SftpConn, realPath string) {
	if host, ok := u.hosts[req.host]; ok {
		if su, ok := host.suMaps[req.su]; ok {
			key := fmt.Sprintf("%s@%s", su.Name, req.host)
			conn, ok := u.sftpClients[key]
			if !ok {
				var err error
				conn, err = u.GetSftpClient(host.asset, su)
				if err != nil {
					logger.Debug("Get Sftp Client err: ", err.Error())
					return nil, ""
				}
				u.sftpClients[key] = conn
			}

			switch strings.ToLower(u.RootPath) {
			case "home", "~", "":
				realPath = filepath.Join(conn.HomeDirPath, strings.TrimPrefix(req.dpath, "/"))
			default:
				realPath = filepath.Join(u.RootPath, strings.TrimPrefix(req.dpath, "/"))
			}
			return conn, realPath
		}
	}
	return
}

func (u *UserSftp) HostHasUniqueSu(hostKey string) (string, bool) {
	if host, ok := u.hosts[hostKey]; ok {
		return host.HasUniqueSu()
	}
	return "", false
}

func (u *UserSftp) validatePermission(su *model.SystemUser, action string) bool {
	for _, pemAction := range su.Actions {
		if pemAction == action || pemAction == model.AllAction {
			return true
		}
	}
	return false
}

func (u *UserSftp) CreateFTPLog(asset *model.Asset, su *model.SystemUser, operate, filename string, isSuccess bool) {
	data := model.FTPLog{
		User:       fmt.Sprintf("%s (%s)", u.User.Name, u.User.Username),
		Hostname:   asset.Hostname,
		OrgID:      asset.OrgID,
		SystemUser: su.Name,
		RemoteAddr: u.Addr,
		Operate:    operate,
		Path:       filename,
		DataStart:  common.CurrentUTCTime(),
		IsSuccess:  isSuccess,
	}
	u.LogChan <- &data
}

func (u *UserSftp) LoopPushFTPLog() {
	ftpLogList := make([]*model.FTPLog, 0, 1024)
	dataChan := make(chan *model.FTPLog)
	go u.SendFTPLog(dataChan)
	defer close(dataChan)
	var timeoutSecond time.Duration
	for {
		switch len(ftpLogList) {
		case 0:
			timeoutSecond = time.Second * 60
		default:
			timeoutSecond = time.Second * 1
		}

		select {
		case <-time.After(timeoutSecond):
		case logData, ok := <-u.LogChan:
			if !ok {
				return
			}
			ftpLogList = append(ftpLogList, logData)
		}

		if len(ftpLogList) > 0 {
			select {
			case dataChan <- ftpLogList[len(ftpLogList)-1]:
				ftpLogList = ftpLogList[:len(ftpLogList)-1]
			default:
			}
		}
	}
}

func (u *UserSftp) SendFTPLog(dataChan <-chan *model.FTPLog) {
	for data := range dataChan {
		for i := 0; i < 4; i++ {
			err := service.PushFTPLog(data)
			if err == nil {
				break
			}
			logger.Debugf("create FTP log err: %s", err.Error())
		}
	}
}

func (u *UserSftp) GetSftpClient(asset *model.Asset, sysUser *model.SystemUser) (conn *SftpConn, err error) {
	sshClient, err := NewClient(u.User, asset, sysUser, config.GetConf().SSHTimeout*time.Second)
	if err != nil {
		return
	}
	sftpClient, err := sftp.NewClient(sshClient.Client)
	if err != nil {
		return
	}

	HomeDirPath, err := sftpClient.Getwd()
	if err != nil {
		return
	}
	conn = &SftpConn{client: sftpClient, conn: sshClient, HomeDirPath: HomeDirPath}
	return conn, err
}

func (u *UserSftp) Close() {
	for _, client := range u.sftpClients {
		client.Close()
	}
	close(u.LogChan)
}

type requestMessage struct {
	host     string
	su       string
	dpath    string
	suUnique bool
}

func NewHostnameDir(asset *model.Asset) *HostnameDir {
	sus := make(map[string]*model.SystemUser)
	for i := 0; i < len(asset.SystemUsers); i++ {
		if asset.SystemUsers[i].Protocol == "ssh" {
			sus[asset.SystemUsers[i].Name] = &asset.SystemUsers[i]
		}
	}
	h := HostnameDir{asset: asset, suMaps: sus}
	return &h
}

type HostnameDir struct {
	asset  *model.Asset
	suMaps map[string]*model.SystemUser
}

func (h *HostnameDir) HasUniqueSu() (string, bool) {
	sus := h.GetSystemUsers()
	if len(sus) == 1 {
		return sus[0].Name, true
	}
	return "", false
}

func (h *HostnameDir) GetSystemUsers() (sus []model.SystemUser) {
	sus = make([] model.SystemUser, 0, len(h.suMaps))
	for _, item := range h.suMaps {
		sus = append(sus, *item)
	}
	model.SortSystemUserByPriority(sus)
	return sus
}

type SftpConn struct {
	HomeDirPath string
	client      *sftp.Client
	conn        *SSHClient
}

func (s *SftpConn) Close() {
	_ = s.client.Close()
	RecycleClient(s.conn)
}

func NewFakeFile(name string, isDir bool) *FakeFileInfo {
	return &FakeFileInfo{
		name:    name,
		modtime: time.Now().UTC(),
		isDir:   isDir,
		size:    int64(0),
	}
}

func NewFakeSymFile(name string) *FakeFileInfo {
	return &FakeFileInfo{
		name:    name,
		modtime: time.Now().UTC(),
		size:    int64(0),
		symlink: name,
	}
}

type FakeFileInfo struct {
	name    string
	isDir   bool
	size    int64
	modtime time.Time
	symlink string
}

func (f *FakeFileInfo) Name() string { return f.name }
func (f *FakeFileInfo) Size() int64  { return f.size }
func (f *FakeFileInfo) Mode() os.FileMode {
	ret := os.FileMode(0644)
	if f.isDir {
		ret = os.FileMode(0755) | os.ModeDir
	}
	if f.symlink != "" {
		ret = os.FileMode(0777) | os.ModeSymlink
	}
	return ret
}
func (f *FakeFileInfo) ModTime() time.Time { return f.modtime }
func (f *FakeFileInfo) IsDir() bool        { return f.isDir }
func (f *FakeFileInfo) Sys() interface{} {
	return &syscall.Stat_t{Uid: 0, Gid: 0}
}

type FileInfoList []os.FileInfo

func (fl FileInfoList) Len() int {
	return len(fl)
}
func (fl FileInfoList) Swap(i, j int)      { fl[i], fl[j] = fl[j], fl[i] }
func (fl FileInfoList) Less(i, j int) bool { return fl[i].Name() < fl[j].Name() }
