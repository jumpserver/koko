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

	com "github.com/jumpserver/koko/pkg/common"
	"github.com/jumpserver/koko/pkg/config"
	"github.com/jumpserver/koko/pkg/jms-sdk-go/common"
	"github.com/jumpserver/koko/pkg/jms-sdk-go/model"
	"github.com/jumpserver/koko/pkg/jms-sdk-go/service"
	"github.com/jumpserver/koko/pkg/logger"
)

type AssetDir struct {
	opts folderOptions

	modeTime time.Time

	user        *model.User
	detailAsset *model.Asset

	suMaps map[string]*model.PermAccount

	sftpClients map[string]*SftpConn // Account stringer

	once sync.Once

	reuse      bool
	ShowHidden bool

	mu sync.Mutex

	jmsService *service.JMService
}

func (ad *AssetDir) Name() string {
	return ad.opts.Name
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
			ad.loadSubAccountDirs()
		}
		if ad.detailAsset == nil {
			ad.loadAssetDetail()
		}
	})
}

func (ad *AssetDir) loadSubAccountDirs() {
	permAccounts, err := ad.jmsService.GetAccountsByUserIdAndAssetId(ad.user.ID, ad.opts.ID)
	if err != nil {
		logger.Errorf("Get asset %s perm accounts err: %s", ad.opts.ID, err)
		return
	}
	ad.suMaps = generateSubAccountsFolderMap(permAccounts)
}

func generateSubAccountsFolderMap(accounts []model.PermAccount) map[string]*model.PermAccount {
	if len(accounts) == 0 {
		return nil
	}
	sus := make(map[string]*model.PermAccount)
	matchFunc := func(s string) bool {
		_, ok := sus[s]
		return ok
	}
	for i := 0; i < len(accounts); i++ {
		//  不支持 @USER 和 @INPUT，
		switch accounts[i].Username {
		case model.InputUser, model.DynamicUser:
			logger.Debugf("Skip @INPUT or @USER account %s", accounts[i].Name)
			continue
		default:
		}
		folderName := cleanFolderName(accounts[i].Name)
		folderName = findAvailableKeyByPaddingSuffix(matchFunc, folderName, paddingCharacter)
		sus[folderName] = &accounts[i]
	}
	return sus
}

func (ad *AssetDir) loadAssetDetail() {
	detailAssets, err := ad.jmsService.GetUserAssetByID(ad.user.ID, ad.opts.ID)
	if err != nil {
		logger.Errorf("Get asset err: %s", err)
		return
	}
	if len(detailAssets) != 1 {
		logger.Errorf("Get asset %s more than one detail err: %s", ad.opts.ID, err)
	}
	ad.detailAsset = &detailAssets[0]
}

func (ad *AssetDir) Create(path string) (*SftpFile, error) {
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
	if !su.Actions.EnableUpload() {
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
	ftpLog := ad.CreateFTPLog(su, operate, filename, isSuccess)
	f := &SftpFile{File: sf, FTPLog: ftpLog}
	return f, err
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
	if !su.Actions.EnableUpload() {
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

func (ad *AssetDir) Open(path string) (*SftpFile, error) {
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
	if !su.Actions.EnableDownload() {
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
	ftpLog := ad.CreateFTPLog(su, operate, filename, isSuccess)
	f := &SftpFile{File: sf, FTPLog: ftpLog}
	return f, err
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
	if !su.Actions.EnableDelete() {
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
	filename := fmt.Sprintf("%s=>%s", oldRealPath, newRealPath)
	operate := model.OperateRename
	err = conn1.client.Rename(oldRealPath, newRealPath)
	if err != nil {
		ad.CreateFTPLog(su, operate, filename, false)
		return err
	}
	if fileInfo, err := conn2.client.Stat(newRealPath); err == nil && fileInfo.IsDir() {
		operate = model.OperateRenameDir
	}
	ad.CreateFTPLog(su, operate, filename, true)
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
	if !su.Actions.EnableDelete() {
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
	if !su.Actions.EnableUpload() {
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

func (ad *AssetDir) GetSFTPAndRealPath(su *model.PermAccount, path string) (conn *SftpConn, realPath string) {
	ad.mu.Lock()
	defer ad.mu.Unlock()
	var ok bool
	conn, ok = ad.sftpClients[su.String()]
	if !ok {
		var err error
		conn, err = ad.GetSftpClient(su)
		if err != nil {
			logger.Errorf("Get Sftp Client err: %s", err.Error())
			return nil, ""
		}
		// todo: 这个地方将创建用户 sftp session
		//conn.token.CreateSession(ad.opts.RemoteAddr, model.LoginFromSSH, model.SFTPType)
		ad.sftpClients[su.String()] = conn
	}
	platform := conn.token.Platform
	sftpRoot := platform.Protocols.GetSftpPath(model.ProtocolSSH)
	accountUsername := su.Username
	username := ad.user.Username
	switch strings.ToLower(sftpRoot) {
	case "home", "~", "":
		realPath = filepath.Join(conn.HomeDirPath, strings.TrimPrefix(path, "/"))
	default:
		//  ${ACCOUNT} 连接的账号用户名, ${USER} 当前用户用户名
		sftpRoot = strings.ReplaceAll(sftpRoot, "${ACCOUNT}", accountUsername)
		sftpRoot = strings.ReplaceAll(sftpRoot, "${USER}", username)
		if strings.Index(sftpRoot, "/") != 0 {
			sftpRoot = fmt.Sprintf("/%s", sftpRoot)
		}
		realPath = filepath.Join(sftpRoot, strings.TrimPrefix(path, "/"))
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

func (ad *AssetDir) GetSftpClient(su *model.PermAccount) (conn *SftpConn, err error) {
	connectToken, err2 := ad.createConnectToken(su)
	if err != nil {
		return nil, fmt.Errorf("get connect token account err: %s", err2)
	}
	return ad.getNewSftpConn(&connectToken)
}

func (ad *AssetDir) createConnectToken(su *model.PermAccount) (model.ConnectToken, error) {
	if ad.opts.token != nil {
		return *ad.opts.token, nil
	}
	req := service.SuperConnectTokenReq{
		UserId:        ad.user.ID,
		AssetId:       ad.opts.ID,
		Account:       su.Alias,
		Protocol:      model.ProtocolSSH,
		ConnectMethod: model.ProtocolSSH,
	}
	// sftp 不支持 ACL 复核的资产，需要从 web terminal 中登录
	tokenInfo, err := ad.jmsService.CreateSuperConnectToken(&req)
	if err != nil {
		msg := err.Error()
		if tokenInfo.Detail != "" {
			msg = tokenInfo.Detail
		}
		logger.Errorf("Create super connect token failed: %s", msg)
		return model.ConnectToken{}, fmt.Errorf("create super connect token failed: %s", msg)
	}
	return ad.jmsService.GetConnectTokenInfo(tokenInfo.ID)
}

func (ad *AssetDir) getNewSftpConn(connectToken *model.ConnectToken) (conn *SftpConn, err error) {
	if ad.detailAsset == nil {
		return nil, errNoSelectAsset
	}
	timeout := config.GlobalConfig.SSHTimeout

	user := connectToken.User
	asset := connectToken.Asset
	account := connectToken.Account
	username := account.Username

	sshAuthOpts := make([]SSHClientOption, 0, 6)
	sshAuthOpts = append(sshAuthOpts, SSHClientUsername(username))
	sshAuthOpts = append(sshAuthOpts, SSHClientHost(asset.Address))
	sshAuthOpts = append(sshAuthOpts, SSHClientPort(asset.ProtocolPort(model.ProtocolSSH)))

	sshAuthOpts = append(sshAuthOpts, SSHClientTimeout(timeout))
	if account.IsSSHKey() {
		if signer, err1 := gossh.ParsePrivateKey([]byte(account.Secret)); err1 == nil {
			sshAuthOpts = append(sshAuthOpts, SSHClientPrivateAuth(signer))
		} else {
			logger.Errorf("ssh private key parse failed: %s", err1)
		}
	} else {
		sshAuthOpts = append(sshAuthOpts, SSHClientPassword(account.Secret))
	}

	if connectToken.Gateway != nil {
		gateway := connectToken.Gateway
		proxyArgs := make([]SSHClientOptions, 0, 1)
		loginAccount := gateway.Account
		port := gateway.Protocols.GetProtocolPort(model.ProtocolSSH)
		proxyArg := SSHClientOptions{
			Host:     gateway.Address,
			Port:     strconv.Itoa(port),
			Username: loginAccount.Username,
			Timeout:  timeout,
		}
		if loginAccount.IsSSHKey() {
			proxyArg.PrivateKey = loginAccount.Secret
		} else {
			proxyArg.Password = loginAccount.Secret
		}
		proxyArgs = append(proxyArgs, proxyArg)
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
	sftpClient, err := NewSftpConn(sess)
	if err != nil {
		logger.Errorf("SSH client(%s) start sftp conn err %s", sshClient, err)
		_ = sess.Close()
		sshClient.ReleaseSession(sess)
		_ = sshClient.Close()
		return nil, err
	}
	go func() {
		_ = sftpClient.Wait()
		sshClient.ReleaseSession(sess)
		_ = sshClient.Close()
		logger.Infof("User %s SSH client(%s) for SFTP release", user.String(), sshClient)
	}()
	homeDirPath, err := sftpClient.Getwd()
	if err != nil {
		logger.Errorf("SSH client sftp (%s) get home dir err %s", sshClient, err)
		_ = sftpClient.Close()
		return nil, err
	}
	logger.Infof("SSH client %s start sftp client session success", sshClient)
	conn = &SftpConn{client: sftpClient, HomeDirPath: homeDirPath, token: connectToken}
	return conn, nil
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

func (ad *AssetDir) CreateFTPLog(su *model.PermAccount, operate, filename string, isSuccess bool) *model.FTPLog {
	data := model.FTPLog{
		ID:         com.UUID(),
		User:       ad.user.String(),
		Asset:      ad.detailAsset.String(),
		OrgID:      ad.detailAsset.OrgID,
		Account:    su.String(),
		RemoteAddr: ad.opts.RemoteAddr,
		Operate:    operate,
		Path:       filename,
		DateStart:  common.NewNowUTCTime(),
		IsSuccess:  isSuccess,
	}
	if err := ad.jmsService.CreateFileOperationLog(data); err != nil {
		logger.Errorf("Create ftp log err: %s", err)
	}
	return &data
}
