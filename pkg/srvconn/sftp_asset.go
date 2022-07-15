package srvconn

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/pkg/sftp"
	gossh "golang.org/x/crypto/ssh"

	"github.com/jumpserver/koko/pkg/config"
	"github.com/jumpserver/koko/pkg/jms-sdk-go/common"
	"github.com/jumpserver/koko/pkg/jms-sdk-go/model"
	"github.com/jumpserver/koko/pkg/jms-sdk-go/service"
	"github.com/jumpserver/koko/pkg/logger"
)

type AssetDir struct {
	ID         string
	folderName string
	addr       string
	modeTime   time.Time

	user        *model.User
	detailAsset *model.Asset
	domain      *model.Domain

	suMaps map[string]*model.SystemUser

	logChan chan<- *model.FTPLog

	sftpClients map[string]*SftpConn // systemUser_id

	once sync.Once

	reuse      bool
	ShowHidden bool

	mu sync.Mutex

	jmsService *service.JMService
}

func (ad *AssetDir) Name() string {
	return ad.folderName
}

func (ad *AssetDir) Size() int64 { return 0 }

func (ad *AssetDir) Mode() os.FileMode {
	if len(ad.suMaps) > 1 {
		return os.FileMode(0444) | os.ModeDir
	}
	return os.FileMode(0644) | os.ModeDir
}

func (ad *AssetDir) ModTime() time.Time { return ad.modeTime }

func (ad *AssetDir) IsDir() bool { return true }

func (ad *AssetDir) Sys() interface{} {
	return &syscall.Stat_t{Uid: 0, Gid: 0}
}

func (ad *AssetDir) loadSystemUsers() {
	ad.once.Do(func() {
		if ad.suMaps == nil {
			ad.loadSubSystemUserDirs()
		}
		//  不同方式创建的系统用户目录，可能没权限action，所以需要校验加载一次
		ad.loadActionsPermission()
		if ad.detailAsset == nil {
			ad.loadAssetDetail()
		}
		if ad.domain == nil {
			ad.loadAssetDomain()
		}
	})
}

func (ad *AssetDir) loadActionsPermission() {
	for i := range ad.suMaps {
		if ad.suMaps[i].Actions != nil {
			continue
		}
		perms, err := ad.jmsService.GetPermission(ad.user.ID, ad.ID, ad.suMaps[i].ID)
		if err != nil {
			logger.Error(err)
			continue
		}
		ad.suMaps[i].Actions = perms.Actions
	}
}

func (ad *AssetDir) loadSubSystemUserDirs() {
	systemUsers, err := ad.jmsService.GetSystemUsersByUserIdAndAssetId(ad.user.ID, ad.ID)
	if err != nil {
		logger.Errorf("Get asset %s systemUsers err: %s", ad.ID, err)
		return
	}
	ad.suMaps = generateSubSystemUsersFolderMap(systemUsers)
}

func generateSubSystemUsersFolderMap(systemUsers []model.SystemUser) map[string]*model.SystemUser {
	if len(systemUsers) == 0 {
		return nil
	}
	sus := make(map[string]*model.SystemUser)
	matchFunc := func(s string) bool {
		_, ok := sus[s]
		return ok
	}
	for i := 0; i < len(systemUsers); i++ {
		folderName := cleanFolderName(systemUsers[i].Name)
		folderName = findAvailableKeyByPaddingSuffix(matchFunc, folderName, paddingCharacter)
		sus[folderName] = &systemUsers[i]
	}
	return sus
}

func (ad *AssetDir) loadAssetDetail() {
	detailAsset, err := ad.jmsService.GetAssetById(ad.ID)
	if err != nil {
		logger.Errorf("Get asset err: %s", err)
		return
	}
	ad.detailAsset = &detailAsset
}

func (ad *AssetDir) loadAssetDomain() {
	if ad.detailAsset != nil && ad.detailAsset.Domain != "" {
		domainGateways, err := ad.jmsService.GetDomainGateways(ad.detailAsset.Domain)
		if err != nil {
			logger.Errorf("Get asset %s domain err: %s", ad.detailAsset.Hostname, err)
			return
		}
		ad.domain = &domainGateways
	}
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
		return nil, errNoSystemUser
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
		return errNoSystemUser
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
		return nil, errNoSystemUser
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
	operate := model.OperateDownload
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
		return nil, errNoSystemUser
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
		return "", errNoSystemUser
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
		return errNoSystemUser
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
		return errNoSystemUser
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
		return errNoSystemUser
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
		return nil, errNoSystemUser
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
			return errNoSystemUser
		}
		folderName = oldPathData[0]
		oldPathData = oldPathData[1:]
		newPathData = newPathData[1:]
	}
	su, ok := ad.suMaps[folderName]
	if !ok {
		return errNoSystemUser
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
	ad.mu.Lock()
	defer ad.mu.Unlock()
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
	if su.Password == "" && su.PrivateKey == "" {
		var info model.SystemUserAuthInfo
		info, err = ad.jmsService.GetSystemUserAuthById(su.ID, ad.ID, ad.user.ID, ad.user.Username)
		if err != nil {
			return nil, err
		}
		su.Username = info.Username
		su.Password = info.Password
		su.PrivateKey = info.PrivateKey
	}

	if ad.reuse {
		if sftpConn, ok := ad.getCacheSftpConn(su); ok {
			return sftpConn, nil
		}
	}

	return ad.getNewSftpConn(su)
}

func (ad *AssetDir) getCacheSftpConn(su *model.SystemUser) (*SftpConn, bool) {
	if ad.detailAsset == nil {
		return nil, false
	}
	var (
		sshClient *SSHClient
		ok        bool
	)
	key := MakeReuseSSHClientKey(ad.user.ID, ad.ID, su.ID, ad.detailAsset.IP, su.Username)
	switch su.Username {
	case "":
		sshClient, ok = searchSSHClientFromCache(key)
		if ok {
			su.Username = sshClient.Cfg.Username
		}
	default:
		sshClient, ok = GetClientFromCache(key)
	}

	if ok {
		logger.Infof("User %s get reuse ssh client(%s)", ad.user, sshClient)
		sess, err := sshClient.AcquireSession()
		if err != nil {
			logger.Errorf("User %s reuse ssh client(%s) new session err: %s", ad.user, sshClient, err)
			return nil, false
		}
		sftpClient, err := NewSftpConn(sess)
		if err != nil {
			_ = sess.Close()
			sshClient.ReleaseSession(sess)
			logger.Errorf("User %s reuse ssh client(%s) start sftp conn err: %s",
				ad.user.String(), sshClient, err)
			return nil, false
		}
		go func() {
			_ = sftpClient.Wait()
			sshClient.ReleaseSession(sess)
			logger.Infof("Reuse ssh client(%s) for SFTP release", sshClient)
		}()
		HomeDirPath, err := sftpClient.Getwd()
		if err != nil {
			logger.Errorf("Reuse ssh client(%s) get home dir err: %s", sshClient, err)
			_ = sftpClient.Close()
			_ = sess.Close()
			return nil, false
		}
		conn := &SftpConn{client: sftpClient, HomeDirPath: HomeDirPath}
		logger.Infof("Reuse ssh client(%s) for SFTP, current ref: %d", sshClient, sshClient.RefCount())
		return conn, true
	}
	logger.Debugf("User %s do not found reuse ssh client for SFTP", ad.user)
	return nil, false
}

func (ad *AssetDir) getNewSftpConn(su *model.SystemUser) (conn *SftpConn, err error) {
	if ad.detailAsset == nil {
		return nil, errNoSelectAsset
	}
	key := MakeReuseSSHClientKey(ad.user.ID, ad.ID, su.ID, ad.detailAsset.IP, su.Username)
	timeout := config.GlobalConfig.SSHTimeout

	sshAuthOpts := make([]SSHClientOption, 0, 6)
	sshAuthOpts = append(sshAuthOpts, SSHClientUsername(su.Username))
	sshAuthOpts = append(sshAuthOpts, SSHClientHost(ad.detailAsset.IP))
	sshAuthOpts = append(sshAuthOpts, SSHClientPort(ad.detailAsset.ProtocolPort(su.Protocol)))
	sshAuthOpts = append(sshAuthOpts, SSHClientPassword(su.Password))
	sshAuthOpts = append(sshAuthOpts, SSHClientTimeout(timeout))
	if su.PrivateKey != "" {
		// 先使用 password 解析 PrivateKey
		if signer, err1 := gossh.ParsePrivateKeyWithPassphrase([]byte(su.PrivateKey),
			[]byte(su.Password)); err1 == nil {
			sshAuthOpts = append(sshAuthOpts, SSHClientPrivateAuth(signer))
		} else {
			// 如果之前使用password解析失败，则去掉 password, 尝试直接解析 PrivateKey 防止错误的passphrase
			if signer, err1 = gossh.ParsePrivateKey([]byte(su.PrivateKey)); err1 == nil {
				sshAuthOpts = append(sshAuthOpts, SSHClientPrivateAuth(signer))
			}
		}
	}
	if ad.domain != nil && len(ad.domain.Gateways) > 0 {
		proxyArgs := make([]SSHClientOptions, 0, len(ad.domain.Gateways))
		for i := range ad.domain.Gateways {
			gateway := ad.domain.Gateways[i]
			proxyArg := SSHClientOptions{
				Host:       gateway.IP,
				Port:       strconv.Itoa(gateway.Port),
				Username:   gateway.Username,
				Password:   gateway.Password,
				Passphrase: gateway.Password, // 兼容 带密码的private_key,
				PrivateKey: gateway.PrivateKey,
				Timeout:    timeout,
			}
			proxyArgs = append(proxyArgs, proxyArg)
		}
		sshAuthOpts = append(sshAuthOpts, SSHClientProxyClient(proxyArgs...))
	}
	sshClient, err := NewSSHClient(sshAuthOpts...)
	if err != nil {
		logger.Errorf("Get new SSH client err: %s", err)
		return nil, err
	}
	sess, err := sshClient.AcquireSession()
	if err != nil {
		logger.Errorf("SSH client(%s) start sftp client session err %s", sshClient, err)
		_ = sshClient.Close()
		return nil, err
	}
	AddClientCache(key, sshClient)
	sftpClient, err := NewSftpConn(sess)
	if err != nil {
		logger.Errorf("SSH client(%s) start sftp conn err %s", sshClient, err)
		_ = sess.Close()
		sshClient.ReleaseSession(sess)
		return nil, err
	}
	go func() {
		_ = sftpClient.Wait()
		sshClient.ReleaseSession(sess)
		logger.Infof("ssh client(%s) for SFTP release", sshClient)
	}()
	HomeDirPath, err := sftpClient.Getwd()
	if err != nil {
		logger.Errorf("SSH client sftp (%s) get home dir err %s", sshClient, err)
		_ = sftpClient.Close()
		return nil, err
	}
	logger.Infof("SSH client %s start sftp client session success", sshClient)
	conn = &SftpConn{client: sftpClient, HomeDirPath: HomeDirPath}
	return conn, err
}

func (ad *AssetDir) parsePath(path string) []string {
	path = strings.TrimPrefix(path, "/")
	return strings.Split(path, "/")
}

func (ad *AssetDir) close() {
	ad.mu.Lock()
	defer ad.mu.Unlock()
	for _, conn := range ad.sftpClients {
		if conn != nil {
			conn.Close()
		}
	}
}

func (ad *AssetDir) CreateFTPLog(su *model.SystemUser, operate, filename string, isSuccess bool) {
	data := model.FTPLog{
		User:       ad.user.String(),
		Hostname:   ad.detailAsset.String(),
		OrgID:      ad.detailAsset.OrgID,
		SystemUser: su.String(),
		RemoteAddr: ad.addr,
		Operate:    operate,
		Path:       filename,
		DateStart:  common.NewNowUTCTime(),
		IsSuccess:  isSuccess,
	}
	ad.logChan <- &data
}
