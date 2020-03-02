package srvconn

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/pkg/sftp"

	"github.com/jumpserver/koko/pkg/common"
	"github.com/jumpserver/koko/pkg/config"
	"github.com/jumpserver/koko/pkg/logger"
	"github.com/jumpserver/koko/pkg/model"
	"github.com/jumpserver/koko/pkg/service"
)

const (
	SearchFolderName = "_Search"
)

type SearchResultDir struct {
	subDirs    map[string]os.FileInfo
	folderName string
	modeTime   time.Time
}

func (sd *SearchResultDir) Name() string {
	return sd.folderName
}

func (sd *SearchResultDir) Size() int64 { return 0 }

func (sd *SearchResultDir) Mode() os.FileMode {
	return os.ModePerm | os.ModeDir
}

func (sd *SearchResultDir) ModTime() time.Time { return sd.modeTime }

func (sd *SearchResultDir) IsDir() bool { return true }

func (sd *SearchResultDir) Sys() interface{} {
	return &syscall.Stat_t{Uid: 0, Gid: 0}
}

func (sd *SearchResultDir) List() (res []os.FileInfo, err error) {
	for _, item := range sd.subDirs {
		res = append(res, item)
	}
	return
}

func (sd *SearchResultDir) SetSubDirs(subDirs map[string]os.FileInfo) {
	if sd.subDirs != nil {
		for _, dir := range sd.subDirs {
			if assetDir, ok := dir.(*AssetDir); ok {
				assetDir.close()
			}
		}
	}
	sd.subDirs = subDirs
}

func (sd *SearchResultDir) close() {
	for _, dir := range sd.subDirs {
		if assetDir, ok := dir.(*AssetDir); ok {
			assetDir.close()
		}
	}
}

type NodeDir struct {
	node       *model.Node
	subDirs    map[string]os.FileInfo
	folderName string
	modeTime   time.Time

	once *sync.Once
}

func (nd *NodeDir) Name() string {
	return nd.folderName
}

func (nd *NodeDir) Size() int64 { return 0 }

func (nd *NodeDir) Mode() os.FileMode {
	return os.ModePerm | os.ModeDir
}
func (nd *NodeDir) ModTime() time.Time { return nd.modeTime }

func (nd *NodeDir) IsDir() bool { return true }

func (nd *NodeDir) Sys() interface{} {
	return &syscall.Stat_t{Uid: 0, Gid: 0}
}

func (nd *NodeDir) List() (res []os.FileInfo, err error) {
	for _, item := range nd.subDirs {
		res = append(res, item)
	}
	return
}

func (nd *NodeDir) loadNodeAsset(uSftp *UserSftpConn) {
	nd.once.Do(func() {
		nodeTrees := service.GetUserNodeTreeWithAsset(uSftp.User.ID, nd.node.ID, "1")
		dirs := map[string]os.FileInfo{}
		for _, item := range nodeTrees {
			typeName, ok := item.Meta["type"].(string)
			if !ok {
				continue
			}
			body, err := json.Marshal(item.Meta[typeName])
			if err != nil {
				continue
			}
			switch typeName {
			case "node":
				node, err := model.ConvertMetaToNode(body)
				if err != nil {
					logger.Errorf("convert node err: %s", err)
					continue
				}
				nodeDir := NewNodeDir(node)
				folderName := nodeDir.folderName
				for {
					_, ok := dirs[folderName]
					if !ok {
						break
					}
					folderName = fmt.Sprintf("%s_", folderName)
				}
				if folderName != nodeDir.folderName {
					nodeDir.folderName = folderName
				}

				dirs[folderName] = &nodeDir
			case "asset":
				asset, err := model.ConvertMetaToAsset(body)
				if err != nil {
					logger.Errorf("convert asset err: %s", err)
					continue
				}
				if !asset.IsSupportProtocol("ssh") {
					continue
				}
				assetDir := NewAssetDir(uSftp.User, asset, uSftp.Addr, uSftp.logChan)
				folderName := assetDir.folderName
				for {
					_, ok := dirs[folderName]
					if !ok {
						break
					}
					folderName = fmt.Sprintf("%s_", folderName)
				}
				if folderName != assetDir.folderName {
					assetDir.folderName = folderName
				}
				dirs[folderName] = &assetDir
			}
		}
		nd.subDirs = dirs
	})
}

func (nd *NodeDir) close() {
	for _, dir := range nd.subDirs {
		if nodeDir, ok := dir.(*NodeDir); ok {
			nodeDir.close()
			continue
		}
		if assetDir, ok := dir.(*AssetDir); ok {
			assetDir.close()
		}

	}
}

func NewNodeDir(node model.Node) NodeDir {
	folderName := node.Value
	if strings.Contains(node.Value, "/") {
		folderName = strings.ReplaceAll(node.Value, "/", "_")
	}
	return NodeDir{
		node:       &node,
		folderName: folderName,
		subDirs:    map[string]os.FileInfo{},
		modeTime:   time.Now().UTC(),
		once:       new(sync.Once),
	}
}

func NewAssetDir(user *model.User, asset model.Asset, addr string, logChan chan<- *model.FTPLog) AssetDir {
	folderName := asset.Hostname
	if strings.Contains(folderName, "/") {
		folderName = strings.ReplaceAll(folderName, "/", "_")
	}
	conf := config.GetConf()
	return AssetDir{
		user:        user,
		asset:       &asset,
		folderName:  folderName,
		modeTime:    time.Now().UTC(),
		addr:        addr,
		suMaps:      nil,
		logChan:     logChan,
		Overtime:    conf.SSHTimeout * time.Second,
		ShowHidden:  conf.ShowHiddenFile,
		reuse:       conf.ReuseConnection,
		sftpClients: map[string]*SftpConn{},
	}
}

type AssetDir struct {
	user       *model.User
	asset      *model.Asset
	folderName string
	modeTime   time.Time
	addr       string

	suMaps map[string]*model.SystemUser

	logChan chan<- *model.FTPLog

	sftpClients map[string]*SftpConn // systemUser_id

	once sync.Once

	reuse      bool
	ShowHidden bool
	Overtime   time.Duration
}

func (ad *AssetDir) Name() string {
	return ad.folderName
}

func (ad *AssetDir) Size() int64 { return 0 }

func (ad *AssetDir) Mode() os.FileMode {
	return os.ModePerm | os.ModeDir
}

func (ad *AssetDir) ModTime() time.Time { return ad.modeTime }

func (ad *AssetDir) IsDir() bool { return true }

func (ad *AssetDir) Sys() interface{} {
	return &syscall.Stat_t{Uid: 0, Gid: 0}
}

func (ad *AssetDir) loadSystemUsers() {
	ad.once.Do(func() {
		sus := make(map[string]*model.SystemUser)
		SystemUsers := service.GetUserAssetSystemUsers(ad.user.ID, ad.asset.ID)
		for i := 0; i < len(SystemUsers); i++ {
			if SystemUsers[i].Protocol == "ssh" {
				ok := true
				folderName := SystemUsers[i].Name
				for ok {
					if _, ok = sus[folderName]; ok {
						folderName = fmt.Sprintf("%s_", folderName)
					}
				}
				sus[folderName] = &SystemUsers[i]
			}
		}
		ad.suMaps = sus
	})
}

func (ad *AssetDir) Create(path string) (*sftp.File, error) {
	pathData := ad.parsePath(path)
	folderName, ok := ad.IsUniqueSu()
	if !ok {
		if len(pathData) == 1 && pathData[0] == "" {
			return nil, sftp.ErrSshFxPermissionDenied
		}
		folderName = pathData[0]
		pathData = pathData[1:]
	}
	su, ok := ad.suMaps[folderName]
	if !ok {
		return nil, sftp.ErrSshFxNoSuchFile
	}
	if !ad.validatePermission(su, model.UploadAction) {
		return nil, sftp.ErrSshFxPermissionDenied
	}

	con, realPath := ad.GetSFTPAndRealPath(su, strings.Join(pathData, "/"))
	if con == nil {
		return nil, sftp.ErrSshFxConnectionLost
	}
	sf, err := con.client.Create(realPath)
	filename := realPath
	isSuccess := false
	operate := model.OperateUpload
	if err == nil {
		isSuccess = true
	}
	ad.CreateFTPLog(su, operate, filename, isSuccess)
	return sf, err
}

func (ad *AssetDir) MkdirAll(path string) (err error) {
	pathData := ad.parsePath(path)
	folderName, ok := ad.IsUniqueSu()
	if !ok {
		if len(pathData) == 1 && pathData[0] == "" {
			return sftp.ErrSshFxPermissionDenied
		}
		folderName = pathData[0]
		pathData = pathData[1:]
	}
	su, ok := ad.suMaps[folderName]
	if !ok {
		return sftp.ErrSshFxNoSuchFile
	}
	if !ad.validatePermission(su, model.UploadAction) {
		return sftp.ErrSshFxPermissionDenied
	}

	con, realPath := ad.GetSFTPAndRealPath(su, strings.Join(pathData, "/"))
	if con == nil {
		return sftp.ErrSshFxConnectionLost
	}
	err = con.client.MkdirAll(realPath)
	filename := realPath
	isSuccess := false
	operate := model.OperateMkdir
	if err == nil {
		isSuccess = true
	}
	ad.CreateFTPLog(su, operate, filename, isSuccess)
	return
}

func (ad *AssetDir) Open(path string) (*sftp.File, error) {
	pathData := ad.parsePath(path)
	folderName, ok := ad.IsUniqueSu()
	if !ok {
		if len(pathData) == 1 && pathData[0] == "" {
			return nil, sftp.ErrSshFxPermissionDenied
		}
		folderName = pathData[0]
		pathData = pathData[1:]
	}
	su, ok := ad.suMaps[folderName]
	if !ok {
		return nil, sftp.ErrSshFxNoSuchFile
	}
	if !ad.validatePermission(su, model.DownloadAction) {
		return nil, sftp.ErrSshFxPermissionDenied
	}
	con, realPath := ad.GetSFTPAndRealPath(su, strings.Join(pathData, "/"))
	if con == nil {
		return nil, sftp.ErrSshFxConnectionLost
	}
	sf, err := con.client.Open(realPath)
	filename := realPath
	isSuccess := false
	operate := model.OperateDownaload
	if err == nil {
		isSuccess = true
	}
	ad.CreateFTPLog(su, operate, filename, isSuccess)
	return sf, err
}

func (ad *AssetDir) ReadDir(path string) (res []os.FileInfo, err error) {
	pathData := ad.parsePath(path)
	folderName, ok := ad.IsUniqueSu()
	if !ok {
		if len(pathData) == 1 && pathData[0] == "" {
			for folderName := range ad.suMaps {
				res = append(res, NewFakeFile(folderName, true))
			}
			return
		}
		folderName = pathData[0]
		pathData = pathData[1:]
	}
	su, ok := ad.suMaps[folderName]
	if !ok {
		return nil, sftp.ErrSshFxNoSuchFile
	}
	if !ad.validatePermission(su, model.ConnectAction) {
		return res, sftp.ErrSshFxPermissionDenied
	}

	con, realPath := ad.GetSFTPAndRealPath(su, strings.Join(pathData, "/"))
	if con == nil {
		return nil, sftp.ErrSshFxConnectionLost
	}
	res, err = con.client.ReadDir(realPath)
	if !ad.ShowHidden {
		noHiddenFiles := make([]os.FileInfo, 0, len(res))
		for i := 0; i < len(res); i++ {
			if !strings.HasPrefix(res[i].Name(), ".") {
				noHiddenFiles = append(noHiddenFiles, res[i])
			}
		}
		return noHiddenFiles, err
	}
	return
}

func (ad *AssetDir) ReadLink(path string) (res string, err error) {
	pathData := ad.parsePath(path)
	if len(pathData) == 1 && pathData[0] == "" {
		return "", sftp.ErrSshFxOpUnsupported
	}
	folderName, ok := ad.IsUniqueSu()
	if !ok {
		folderName = pathData[0]
		pathData = pathData[1:]
	}
	su, ok := ad.suMaps[folderName]
	if !ok {
		return "", sftp.ErrSshFxNoSuchFile
	}
	if !ad.validatePermission(su, model.ConnectAction) {
		return res, sftp.ErrSshFxPermissionDenied
	}

	con, realPath := ad.GetSFTPAndRealPath(su, strings.Join(pathData, "/"))
	if con == nil {
		return "", sftp.ErrSshFxConnectionLost
	}
	res, err = con.client.ReadLink(realPath)
	return
}

func (ad *AssetDir) RemoveDirectory(path string) (err error) {
	pathData := ad.parsePath(path)
	folderName, ok := ad.IsUniqueSu()
	if !ok {
		if len(pathData) == 1 && pathData[0] == "" {
			return sftp.ErrSshFxPermissionDenied
		}
		folderName = pathData[0]
		pathData = pathData[1:]
	}
	su, ok := ad.suMaps[folderName]
	if !ok {
		return sftp.ErrSshFxNoSuchFile
	}
	if !ad.validatePermission(su, model.UploadAction) {
		return sftp.ErrSshFxPermissionDenied
	}
	con, realPath := ad.GetSFTPAndRealPath(su, strings.Join(pathData, "/"))
	if con == nil {
		return sftp.ErrSshFxConnectionLost
	}
	err = ad.removeDirectoryAll(con.client, realPath)
	filename := realPath
	isSuccess := false
	operate := model.OperateRemoveDir
	if err == nil {
		isSuccess = true
	}
	ad.CreateFTPLog(su, operate, filename, isSuccess)
	return
}

func (ad *AssetDir) Rename(oldNamePath, newNamePath string) (err error) {
	oldPathData := ad.parsePath(oldNamePath)
	newPathData := ad.parsePath(newNamePath)

	folderName, ok := ad.IsUniqueSu()
	if !ok {
		if oldPathData[0] != newPathData[0] {
			return sftp.ErrSshFxNoSuchFile
		}
		folderName = oldPathData[0]
		oldPathData = oldPathData[1:]
		newPathData = newPathData[1:]
	}
	su, ok := ad.suMaps[folderName]
	if !ok {
		return sftp.ErrSshFxNoSuchFile
	}
	conn1, oldRealPath := ad.GetSFTPAndRealPath(su, strings.Join(oldPathData, "/"))
	conn2, newRealPath := ad.GetSFTPAndRealPath(su, strings.Join(newPathData, "/"))
	if conn1 != conn2 {
		return sftp.ErrSshFxOpUnsupported
	}

	err = conn1.client.Rename(oldRealPath, newRealPath)

	filename := fmt.Sprintf("%s=>%s", oldRealPath, newRealPath)
	isSuccess := false
	operate := model.OperateRename
	if err == nil {
		isSuccess = true
	}
	ad.CreateFTPLog(su, operate, filename, isSuccess)
	return
}

func (ad *AssetDir) Remove(path string) (err error) {
	pathData := ad.parsePath(path)
	folderName, ok := ad.IsUniqueSu()
	if !ok {
		if len(pathData) == 1 && pathData[0] == "" {
			return sftp.ErrSshFxPermissionDenied
		}
		folderName = pathData[0]
		pathData = pathData[1:]
	}
	su, ok := ad.suMaps[folderName]
	if !ok {
		return sftp.ErrSshFxNoSuchFile
	}
	if !ad.validatePermission(su, model.UploadAction) {
		return sftp.ErrSshFxPermissionDenied
	}
	con, realPath := ad.GetSFTPAndRealPath(su, strings.Join(pathData, "/"))
	if con == nil {
		return sftp.ErrSshFxConnectionLost
	}
	err = con.client.Remove(realPath)

	filename := realPath
	isSuccess := false
	operate := model.OperateDelete
	if err == nil {
		isSuccess = true
	}
	ad.CreateFTPLog(su, operate, filename, isSuccess)
	return
}

func (ad *AssetDir) Stat(path string) (res os.FileInfo, err error) {
	pathData := ad.parsePath(path)
	if len(pathData) == 1 && pathData[0] == "" {
		return ad, nil
	}
	folderName, ok := ad.IsUniqueSu()
	if !ok {
		folderName = pathData[0]
		pathData = pathData[1:]
	}
	su, ok := ad.suMaps[folderName]
	if !ok {
		return nil, sftp.ErrSshFxNoSuchFile
	}
	if !ad.validatePermission(su, model.ConnectAction) {
		return res, sftp.ErrSshFxPermissionDenied
	}
	con, realPath := ad.GetSFTPAndRealPath(su, strings.Join(pathData, "/"))
	if con == nil {
		return nil, sftp.ErrSshFxConnectionLost
	}
	res, err = con.client.Stat(realPath)
	return
}

func (ad *AssetDir) Symlink(oldNamePath, newNamePath string) (err error) {
	oldPathData := ad.parsePath(oldNamePath)
	newPathData := ad.parsePath(newNamePath)

	folderName, ok := ad.IsUniqueSu()
	if !ok {
		if oldPathData[0] != newPathData[0] {
			return sftp.ErrSshFxNoSuchFile
		}
		folderName = oldPathData[0]
		oldPathData = oldPathData[1:]
		newPathData = newPathData[1:]
	}
	su, ok := ad.suMaps[folderName]
	if !ok {
		return sftp.ErrSshFxNoSuchFile
	}
	if !ad.validatePermission(su, model.UploadAction) {
		return sftp.ErrSshFxPermissionDenied
	}
	conn1, oldRealPath := ad.GetSFTPAndRealPath(su, strings.Join(oldPathData, "/"))
	conn2, newRealPath := ad.GetSFTPAndRealPath(su, strings.Join(newPathData, "/"))
	if conn1 != conn2 {
		return sftp.ErrSshFxOpUnsupported
	}
	err = conn1.client.Symlink(oldRealPath, newRealPath)
	filename := fmt.Sprintf("%s=>%s", oldRealPath, newRealPath)
	isSuccess := false
	operate := model.OperateSymlink
	if err == nil {
		isSuccess = true
	}
	ad.CreateFTPLog(su, operate, filename, isSuccess)
	return
}

func (ad *AssetDir) removeDirectoryAll(conn *sftp.Client, path string) error {
	var err error
	var files []os.FileInfo
	files, err = conn.ReadDir(path)
	if err != nil {
		return err
	}
	for _, item := range files {
		realPath := filepath.Join(path, item.Name())

		if item.IsDir() {
			err = ad.removeDirectoryAll(conn, realPath)
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

func (ad *AssetDir) GetSFTPAndRealPath(su *model.SystemUser, path string) (conn *SftpConn, realPath string) {
	var ok bool
	conn, ok = ad.sftpClients[su.ID]
	if !ok {
		var err error
		conn, err = ad.GetSftpClient(su)
		if err != nil {
			logger.Errorf("Get Sftp Client err: %s", err.Error())
			return nil, ""
		}
		ad.sftpClients[su.ID] = conn
	}

	switch strings.ToLower(su.SftpRoot) {
	case "home", "~", "":
		realPath = filepath.Join(conn.HomeDirPath, strings.TrimPrefix(path, "/"))
	default:
		if strings.Index(su.SftpRoot, "/") != 0 {
			su.SftpRoot = fmt.Sprintf("/%s", su.SftpRoot)
		}
		realPath = filepath.Join(su.SftpRoot, strings.TrimPrefix(path, "/"))
	}
	return
}

func (ad *AssetDir) IsUniqueSu() (folderName string, ok bool) {
	sus := ad.getSubFolderNames()
	if len(sus) == 1 {
		return sus[0], true
	}
	return
}

func (ad *AssetDir) getSubFolderNames() []string {
	sus := make([]string, 0, len(ad.suMaps))
	for folderName := range ad.suMaps {
		sus = append(sus, folderName)
	}
	return sus
}

func (ad *AssetDir) validatePermission(su *model.SystemUser, action string) bool {
	for _, pemAction := range su.Actions {
		if pemAction == action || pemAction == model.AllAction {
			return true
		}
	}
	return false
}

func (ad *AssetDir) GetSftpClient(su *model.SystemUser) (conn *SftpConn, err error) {
	var (
		sshClient *SSHClient
		ok        bool
	)
	var needNewClient = true
	if su.Password == "" && su.PrivateKey == "" && su.LoginMode != model.LoginModeManual {
		var info model.SystemUserAuthInfo
		if su.UsernameSameWithUser {
			info = service.GetUserAssetAuthInfo(su.ID, ad.asset.ID, ad.user.ID, ad.user.Username)
			su.Username = ad.user.Username
		} else {
			info = service.GetSystemUserAssetAuthInfo(su.ID, ad.asset.ID, )
		}
		su.Password = info.Password
		su.PrivateKey = info.PrivateKey
	}

	if ad.reuse {
		key := MakeReuseSSHClientKey(ad.user, ad.asset, su)
		switch su.Username {
		case "":
			sshClient, ok = searchSSHClientFromCache(key)
			if ok {
				su.Username = sshClient.username
			}
		default:
			sshClient, ok = GetClientFromCache(key)
		}

		if ok {
			needNewClient = false
			logger.Infof("Reuse connection for SFTP: %s->%s@%s. SSH client %p current ref: %d",
				ad.user.Username, sshClient.username, ad.asset.IP, sshClient, sshClient.RefCount())
		}
	}

	if needNewClient {
		sshClient, err = NewClient(ad.user, ad.asset, su, ad.Overtime, ad.reuse)
		if err != nil {
			logger.Errorf("Get new SSH client err: %s", err)
			return
		}
	}

	sftpClient, err := sftp.NewClient(sshClient.client)
	if err != nil {
		logger.Errorf("SSH client %p start sftp client session err %s", sshClient, err)
		RecycleClient(sshClient)
		return nil, err
	}

	HomeDirPath, err := sftpClient.Getwd()
	if err != nil {
		logger.Errorf("SSH client %p get home dir err %s", sshClient, err)
		_ = sftpClient.Close()
		RecycleClient(sshClient)
		return nil, err
	}
	logger.Infof("SSH client %p start sftp client session success", sshClient)
	conn = &SftpConn{client: sftpClient, conn: sshClient, HomeDirPath: HomeDirPath}
	return conn, err
}

func (ad *AssetDir) parsePath(path string) []string {
	path = strings.TrimPrefix(path, "/")
	return strings.Split(path, "/")
}

func (ad *AssetDir) close() {
	for _, conn := range ad.sftpClients {
		if conn != nil {
			conn.Close()
		}
	}
}

func (ad *AssetDir) CreateFTPLog(su *model.SystemUser, operate, filename string, isSuccess bool) {
	data := model.FTPLog{
		User:       fmt.Sprintf("%s (%s)", ad.user.Name, ad.user.Username),
		Hostname:   ad.asset.Hostname,
		OrgID:      ad.asset.OrgID,
		SystemUser: su.Name,
		RemoteAddr: ad.addr,
		Operate:    operate,
		Path:       filename,
		DataStart:  common.CurrentUTCTime(),
		IsSuccess:  isSuccess,
	}
	ad.logChan <- &data
}

type SftpConn struct {
	HomeDirPath string
	client      *sftp.Client
	conn        *SSHClient
}

func (s *SftpConn) Close() {
	if s.client == nil {
		return
	}
	_ = s.client.Close()
	RecycleClient(s.conn)
}

func NewFakeFile(name string, isDir bool) *FakeFileInfo {
	return &FakeFileInfo{
		name:    name,
		modTime: time.Now().UTC(),
		isDir:   isDir,
		size:    int64(0),
	}
}

func NewFakeSymFile(name string) *FakeFileInfo {
	return &FakeFileInfo{
		name:    name,
		modTime: time.Now().UTC(),
		size:    int64(0),
		symlink: name,
	}
}

type FakeFileInfo struct {
	name    string
	isDir   bool
	size    int64
	modTime time.Time
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
func (f *FakeFileInfo) ModTime() time.Time { return f.modTime }
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
