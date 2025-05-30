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

	"github.com/jumpserver-dev/sdk-go/common"
	"github.com/jumpserver-dev/sdk-go/model"
	"github.com/jumpserver-dev/sdk-go/service"
	com "github.com/jumpserver/koko/pkg/common"
	"github.com/jumpserver/koko/pkg/config"
	"github.com/jumpserver/koko/pkg/logger"
	"github.com/jumpserver/koko/pkg/session"
)

type AssetDir struct {
	opts folderOptions

	modeTime time.Time

	user        *model.User
	detailAsset *model.PermAsset
	once        sync.Once
	suMaps      map[string]*model.PermAccount

	mu           sync.Mutex
	sftpSessions sync.Map

	ShowHidden bool

	jmsService *service.JMService

	isFromWebTerminal bool
	CurrentPath       string
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
	permAssetDetail, err := ad.jmsService.GetUserPermAssetDetailById(ad.user.ID, ad.opts.ID)
	if err != nil {
		logger.Errorf("Get asset %s perm asset detail err: %s", ad.opts.ID, err)
		return
	}
	accounts := make([]model.PermAccount, 0, len(permAssetDetail.PermedAccounts))
	for i := 0; i < len(permAssetDetail.PermedAccounts); i++ {
		pAccount := permAssetDetail.PermedAccounts[i]
		if ad.opts.accountUsername != "" && ad.opts.accountUsername != pAccount.Username {
			continue
		}
		accounts = append(accounts, pAccount)
	}
	ad.suMaps = generateSubAccountsFolderMap(accounts)

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
		case model.InputUser, model.DynamicUser, model.ANONUser:
			logger.Debugf("Skip unSupported account %s", accounts[i].Name)
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
		return nil, errNoAccountUser
	}
	if !su.Actions.EnableUpload() {
		return nil, sftp.ErrSshFxPermissionDenied
	}

	con, realPath := ad.GetSFTPAndRealPath(su, strings.Join(pathData, "/"))
	if con == nil || con.isClosed {
		return nil, sftp.ErrSshFxConnectionLost
	}
	con.IncreaseRef()
	for !con.IsOverwriteFile() {
		if exitFile := IsExistPath(con.client, realPath); !exitFile {
			break
		}
		oldPath := realPath
		ext := filepath.Ext(realPath)
		realPath = fmt.Sprintf("%s_duplicate_%s%s", realPath[:len(realPath)-len(ext)],
			strconv.FormatInt(time.Now().Unix(), 10), realPath[len(realPath)-len(ext):])
		logger.Infof("Change duplicate dir path %s to %s", oldPath, realPath)
	}
	sf, err := con.client.Create(realPath)
	filename := realPath
	isSuccess := false
	operate := model.OperateUpload
	if err == nil {
		isSuccess = true
	}
	ftpLog := ad.CreateFTPLog(su, operate, filename, isSuccess)
	f := &SftpFile{File: sf, FTPLog: ftpLog, cleanupFunc: con.DecreaseRef}
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
		return errNoAccountUser
	}
	if !su.Actions.EnableUpload() {
		return sftp.ErrSshFxPermissionDenied
	}

	con, realPath := ad.GetSFTPAndRealPath(su, strings.Join(pathData, "/"))
	if con == nil || con.isClosed {
		return sftp.ErrSshFxConnectionLost
	}
	for !con.IsOverwriteFile() {
		if exitFile := IsExistPath(con.client, realPath); !exitFile {
			break
		}
		oldPath := realPath
		realPath = fmt.Sprintf("%s_duplicate__%s", realPath,
			strconv.FormatInt(time.Now().Unix(), 10))
		logger.Infof("Change duplicate dir path %s to %s", oldPath, realPath)
	}
	con.IncreaseRef()
	defer con.DecreaseRef()
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
		return nil, errNoAccountUser
	}
	if !su.Actions.EnableDownload() {
		return nil, sftp.ErrSshFxPermissionDenied
	}
	con, realPath := ad.GetSFTPAndRealPath(su, strings.Join(pathData, "/"))
	if con == nil {
		return nil, sftp.ErrSshFxConnectionLost
	}
	con.IncreaseRef()
	sf, err := con.client.Open(realPath)
	filename := realPath
	isSuccess := false
	operate := model.OperateDownload
	if err == nil {
		isSuccess = true
	}
	ftpLog := ad.CreateFTPLog(su, operate, filename, isSuccess)
	f := &SftpFile{File: sf, FTPLog: ftpLog, cleanupFunc: con.DecreaseRef}
	return f, err
}

func (ad *AssetDir) ReadDir(path string) (res []os.FileInfo, err error) {
	pathData := ad.parsePath(path)
	folderName, ok := ad.IsUniqueSu()
	if !ok && !ad.isFromWebTerminal {
		if len(pathData) == 1 && pathData[0] == "" {
			for accountName := range ad.suMaps {
				res = append(res, NewFakeFile(accountName, true))
			}
			return
		}
		folderName = pathData[0]
		pathData = pathData[1:]
	}
	su, ok := ad.suMaps[folderName]
	if !ok {
		return nil, errNoAccountUser
	}

	con, realPath := ad.GetSFTPAndRealPath(su, strings.Join(pathData, "/"))
	ad.CurrentPath = realPath

	if con == nil || con.isClosed {
		return nil, sftp.ErrSshFxConnectionLost
	}
	con.IncreaseRef()
	defer con.DecreaseRef()
	res, err = con.client.ReadDir(realPath)
	isRootAccount := con.token.Account.Username == "root"
	fileInfoList := make([]os.FileInfo, 0, len(res))
	for i := 0; i < len(res); i++ {
		info := NewSftpFileInfo(res[i], isRootAccount, ad.isFromWebTerminal)
		if !ad.ShowHidden && strings.HasPrefix(info.Name(), ".") {
			continue
		}
		//  兼容 MobaXterm, 打开软连接目录
		if info.Mode()&os.ModeSymlink != 0 {
			linkPath := filepath.Join(realPath, info.Name())
			linkInfo, err1 := con.client.Stat(linkPath)
			if err1 != nil {
				logger.Errorf("ReadDir get link info err: %s", err1)
				continue
			}
			info = NewSftpFileInfo(linkInfo, isRootAccount, ad.isFromWebTerminal)
		}
		fileInfoList = append(fileInfoList, info)
	}
	return fileInfoList, err
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
		return "", errNoAccountUser
	}

	con, realPath := ad.GetSFTPAndRealPath(su, strings.Join(pathData, "/"))
	if con == nil || con.isClosed {
		return "", sftp.ErrSshFxConnectionLost
	}
	con.IncreaseRef()
	defer con.DecreaseRef()
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
		return errNoAccountUser
	}
	if !su.Actions.EnableDelete() {
		return sftp.ErrSshFxPermissionDenied
	}
	con, realPath := ad.GetSFTPAndRealPath(su, strings.Join(pathData, "/"))
	if con == nil || con.isClosed {
		return sftp.ErrSshFxConnectionLost
	}
	if con.IsRootPath(realPath) {
		logger.Errorf("Diable to remove root setting path %s", realPath)
		return sftp.ErrSshFxPermissionDenied
	}
	con.IncreaseRef()
	defer con.DecreaseRef()
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
		return errNoAccountUser
	}
	if !su.Actions.EnableUpload() {
		return sftp.ErrSshFxPermissionDenied
	}
	conn1, oldRealPath := ad.GetSFTPAndRealPath(su, strings.Join(oldPathData, "/"))
	conn2, newRealPath := ad.GetSFTPAndRealPath(su, strings.Join(newPathData, "/"))
	if conn1 != conn2 {
		return sftp.ErrSshFxOpUnsupported
	}
	if conn1 == nil || conn1.isClosed {
		return sftp.ErrSshFxConnectionLost
	}
	conn1.IncreaseRef()
	defer conn1.DecreaseRef()
	filename := fmt.Sprintf("%s=>%s", oldRealPath, newRealPath)
	operate := model.OperateRename
	err = conn1.client.Rename(oldRealPath, newRealPath)
	if err != nil {
		ad.CreateFTPLog(su, operate, filename, false)
		return err
	}
	if fileInfo, err1 := conn2.client.Stat(newRealPath); err1 == nil && fileInfo.IsDir() {
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
		return errNoAccountUser
	}
	if !su.Actions.EnableDelete() {
		return sftp.ErrSshFxPermissionDenied
	}
	con, realPath := ad.GetSFTPAndRealPath(su, strings.Join(pathData, "/"))
	if con == nil || con.isClosed {
		return sftp.ErrSshFxConnectionLost
	}
	con.IncreaseRef()
	defer con.DecreaseRef()
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
		return nil, errNoAccountUser
	}
	con, realPath := ad.GetSFTPAndRealPath(su, strings.Join(pathData, "/"))
	if con == nil || con.isClosed {
		return nil, sftp.ErrSshFxConnectionLost
	}
	con.IncreaseRef()
	defer con.DecreaseRef()
	res, err = con.client.Stat(realPath)
	isRootAccount := con.token.Account.Username == "root"
	return NewSftpFileInfo(res, isRootAccount, ad.isFromWebTerminal), err
}

func (ad *AssetDir) Symlink(oldNamePath, newNamePath string) (err error) {
	oldPathData := ad.parsePath(oldNamePath)
	newPathData := ad.parsePath(newNamePath)

	folderName, ok := ad.IsUniqueSu()
	if !ok {
		if oldPathData[0] != newPathData[0] {
			return errNoAccountUser
		}
		folderName = oldPathData[0]
		oldPathData = oldPathData[1:]
		newPathData = newPathData[1:]
	}
	su, ok := ad.suMaps[folderName]
	if !ok {
		return errNoAccountUser
	}
	if !su.Actions.EnableUpload() {
		return sftp.ErrSshFxPermissionDenied
	}
	conn1, oldRealPath := ad.GetSFTPAndRealPath(su, strings.Join(oldPathData, "/"))
	conn2, newRealPath := ad.GetSFTPAndRealPath(su, strings.Join(newPathData, "/"))
	if conn1 != conn2 {
		return sftp.ErrSshFxOpUnsupported
	}
	conn1.IncreaseRef()
	defer conn1.DecreaseRef()
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

func (ad *AssetDir) checkExpired() {
	ad.sftpSessions.Range(func(key, value interface{}) bool {
		if value == nil {
			return true
		}
		conn := value.(*SftpSession)
		if conn.isClosed {
			return true
		}
		if conn.client == nil {
			return true
		}
		if conn.IsExpired() {
			conn.CloseWithReason(model.ReasonErrIdleDisconnect)
			logger.Infof("SFTP session %s idle timeout closed", conn.sess.ID)
		}
		return true
	})
}

func (ad *AssetDir) GetRealPath(sftpSess *SftpSession, path string) string {
	realPath := filepath.Join(sftpSess.rootDirPath, strings.TrimPrefix(path, "/"))
	if ad.isFromWebTerminal && path != "" && strings.HasPrefix(path, sftpSess.rootDirPath) {
		return path
	}
	return realPath
}

func (ad *AssetDir) GetSFTPAndRealPath(su *model.PermAccount, path string) (conn *SftpConn, realPath string) {
	ad.mu.Lock()
	defer ad.mu.Unlock()
	key := su.String()
	if val, ok := ad.sftpSessions.Load(key); ok {
		sftpSess := val.(*SftpSession)
		realPath = ad.GetRealPath(sftpSess, path)
		return sftpSess.SftpConn, realPath
	}

	sftpSession, err := ad.createSftpSession(su)
	if err != nil {
		logger.Errorf("Create sftp session err: %s", err.Error())
		return nil, ""
	}
	ad.sftpSessions.Store(key, sftpSession)

	realPath = ad.GetRealPath(sftpSession, path)
	return sftpSession.SftpConn, realPath
}

func (ad *AssetDir) createSftpSession(su *model.PermAccount) (sftpSess *SftpSession, err error) {
	conn, err := ad.GetSftpClient(su)
	if err != nil {
		return nil, err
	}
	reqSession := conn.token.CreateSession(ad.opts.RemoteAddr, ad.opts.fromType, model.SFTPType)
	respSession, err1 := ad.jmsService.CreateSession(reqSession)
	if err1 != nil {
		logger.Errorf("Create sftp Session err: %s", err1.Error())
		return nil, err1
	}
	respSession.TokenId = conn.token.Id
	sftpSession := &SftpSession{SftpConn: conn, sess: &respSession, jmsService: ad.jmsService}
	terminalFunc := func(task *model.TerminalTask) error {
		switch task.Name {
		case model.TaskKillSession:
			sftpSession.CloseWithReason(model.ReasonErrAdminTerminate)
			return nil
		case model.TaskPermExpired:
			sftpSession.CloseWithReason(model.ReasonErrPermissionExpired)
			return nil
		case model.TaskPermValid:
			return nil
		}
		return fmt.Errorf("sftp session not support task: %s", task.Name)
	}
	traceSession := session.NewSession(&respSession, terminalFunc)
	session.AddSession(traceSession)
	ad.recordSessionLifecycle(traceSession.ID, model.AssetConnectSuccess, "")

	go func() {
		_ = conn.client.Wait()
		sftpSession.Close()
		logger.Infof("SFTP session %s closed", sftpSession.sess.ID)
	}()
	return sftpSession, nil
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
	if err2 != nil {
		return nil, fmt.Errorf("get connect token account err: %s", err2)
	}
	return ad.getNewSftpConn(&connectToken, su)
}

func (ad *AssetDir) createConnectToken(su *model.PermAccount) (model.ConnectToken, error) {
	if ad.opts.token != nil {
		return *ad.opts.token, nil
	}
	req := service.SuperConnectTokenReq{
		UserId:        ad.user.ID,
		AssetId:       ad.opts.ID,
		Account:       su.Alias,
		Protocol:      model.ProtocolSFTP,
		ConnectMethod: model.ProtocolSFTP,
		RemoteAddr:    ad.opts.RemoteAddr,
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
	return ad.jmsService.GetConnectTokenInfo(tokenInfo.ID, true)
}

func (ad *AssetDir) getNewSftpConn(connectToken *model.ConnectToken,
	su *model.PermAccount) (conn *SftpConn, err error) {
	if ad.detailAsset == nil {
		return nil, errNoSelectAsset
	}
	timeout := config.GlobalConfig.SSHTimeout
	sshClient, err := NewSSHClientWithToken(connectToken, timeout)
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
	homeDirPath, err := sftpClient.Getwd()
	if err != nil {
		logger.Errorf("SSH client sftp (%s) get home dir err %s", sshClient, err)
		_ = sftpClient.Close()
		sshClient.ReleaseSession(sess)
		_ = sshClient.Close()
		return nil, err
	}
	logger.Infof("SSH client %s start sftp client session success", sshClient)

	platform := connectToken.Platform
	sftpRoot := platform.Protocols.GetSftpPath(model.ProtocolSFTP)
	accountUsername := su.Username
	username := ad.user.Username
	switch strings.ToLower(sftpRoot) {
	case "home", "~", "":
		sftpRoot = homeDirPath
	default:
		//  ${ACCOUNT} 连接的账号用户名, ${USER} 当前用户用户名, ${HOME} 当前家目录
		homeDir := homeDirPath
		sftpRoot = strings.ReplaceAll(sftpRoot, "${ACCOUNT}", accountUsername)
		sftpRoot = strings.ReplaceAll(sftpRoot, "${USER}", username)
		sftpRoot = strings.ReplaceAll(sftpRoot, "${HOME}", homeDir)
		if strings.Index(sftpRoot, "/") != 0 {
			sftpRoot = fmt.Sprintf("/%s", sftpRoot)
		}
	}
	maxIdleInt := ad.opts.terminalCfg.MaxIdleTime
	conn = &SftpConn{
		sshClient:   sshClient,
		sshSession:  sess,
		permAccount: su,
		rootDirPath: sftpRoot,
		client:      sftpClient,
		HomeDirPath: homeDirPath,
		token:       connectToken,
		maxIdleTime: time.Duration(maxIdleInt) * time.Minute,
	}
	return conn, nil
}

func NewSSHClientWithToken(connectToken *model.ConnectToken, timeout int) (*SSHClient, error) {
	asset := connectToken.Asset
	account := connectToken.Account
	username := account.Username
	protocol := connectToken.Protocol

	sshAuthOpts := make([]SSHClientOption, 0, 6)
	sshAuthOpts = append(sshAuthOpts, SSHClientUsername(username))
	sshAuthOpts = append(sshAuthOpts, SSHClientHost(asset.Address))
	sshAuthOpts = append(sshAuthOpts, SSHClientPort(asset.ProtocolPort(protocol)))
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
	return NewSSHClient(sshAuthOpts...)
}

func (ad *AssetDir) parsePath(path string) []string {
	if ad.isFromWebTerminal {
		return []string{path}
	}
	path = strings.TrimPrefix(path, "/")
	return strings.Split(path, "/")
}

func (ad *AssetDir) close() {
	ad.sftpSessions.Range(func(key, value interface{}) bool {
		if conn, ok := value.(*SftpSession); ok {
			conn.Close()
		}
		return true
	})
}

func (ad *AssetDir) CreateFTPLog(su *model.PermAccount, operate, filename string, isSuccess bool) *model.FTPLog {
	sessionId := ""
	if val, ok := ad.sftpSessions.Load(su.String()); ok {
		traceSession := val.(*SftpSession)
		sessionId = traceSession.sess.ID
	} else {
		logger.Errorf("Not found sftp session for asset %s account %s",
			ad.detailAsset.String(), su.String())
	}

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
		Session:    sessionId,
	}
	if err := ad.jmsService.CreateFileOperationLog(data); err != nil {
		logger.Errorf("Create ftp log err: %s", err)
	}
	return &data
}

func (ad *AssetDir) recordSessionLifecycle(sid string, event model.LifecycleEvent, reason string) {
	logObj := model.SessionLifecycleLog{Reason: reason}
	if err := ad.jmsService.RecordSessionLifecycleLog(sid, event, logObj); err != nil {
		logger.Errorf("Update session %s lifecycle %s failed: %s", sid, event, err)
	}
}

func IsExistPath(client *sftp.Client, path string) bool {
	_, err := client.Stat(path)
	return err == nil
}

func NewSftpFileInfo(info os.FileInfo, isRoot, isFromWebTerminal bool) os.FileInfo {
	if !isRoot {
		return info
	}
	return &SftpFileInfo{info: info, isRoot: isRoot, isFromWebTerminal: isFromWebTerminal}
}

type SftpFileInfo struct {
	info              os.FileInfo
	isRoot            bool
	isFromWebTerminal bool
}

func (ad *SftpFileInfo) Name() string {
	return ad.info.Name()
}

func (ad *SftpFileInfo) Size() int64 {
	return ad.info.Size()
}

/*
	特殊处理：
		如果是 root 账号，获取的目录信息，手动修改其文件权限可读写,
		允许其在 web sftp 可以上传文件
*/

func (ad *SftpFileInfo) Mode() os.FileMode {
	if ad.isFromWebTerminal {
		return ad.info.Mode()
	}
	if ad.isRoot && ad.info.IsDir() {
		return ad.info.Mode() | os.ModePerm
	}
	return ad.info.Mode()
}

func (ad *SftpFileInfo) ModTime() time.Time {
	return ad.info.ModTime()
}

func (ad *SftpFileInfo) IsDir() bool { return ad.info.IsDir() }

func (ad *SftpFileInfo) Sys() interface{} {
	return ad.info.Sys()
}
