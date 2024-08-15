package srvconn

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"syscall"
	"time"

	"github.com/pkg/sftp"

	"github.com/jumpserver/koko/pkg/jms-sdk-go/model"
	"github.com/jumpserver/koko/pkg/jms-sdk-go/service"
	"github.com/jumpserver/koko/pkg/logger"
)

var errNoSelectAsset = errors.New("please select one of the assets")

type UserSftpConn struct {
	User      *model.User
	Addr      string
	loginFrom model.LabelField

	Dirs map[string]os.FileInfo

	modeTime time.Time

	closed    chan struct{}
	searchDir *SearchResultDir

	jmsService *service.JMService

	opts *userSftpOption

	assetDir *AssetDir
}

func (u *UserSftpConn) ReadDir(path string) (res []os.FileInfo, err error) {
	if u.assetDir != nil {
		return u.assetDir.ReadDir(path)
	}
	fi, restPath := u.ParsePath(path)
	if rootDir, ok := fi.(*UserSftpConn); ok {
		return rootDir.List()
	}

	if nodeDir, ok := fi.(*NodeDir); ok {
		return nodeDir.List()
	}
	if assetDir, ok := fi.(*AssetDir); ok {
		return assetDir.ReadDir(restPath)
	}

	return nil, errNoSelectAsset
}

func (u *UserSftpConn) Stat(path string) (res os.FileInfo, err error) {
	if u.assetDir != nil {
		return u.assetDir.Stat(path)
	}

	fi, restPath := u.ParsePath(path)
	if rootDir, ok := fi.(*UserSftpConn); ok {
		return rootDir, nil
	}

	if nodeDir, ok := fi.(*NodeDir); ok {
		return nodeDir, nil
	}
	if assetDir, ok := fi.(*AssetDir); ok {
		return assetDir.Stat(restPath)
	}

	return nil, errNoSelectAsset
}

func (u *UserSftpConn) ReadLink(path string) (name string, err error) {
	if u.assetDir != nil {
		return u.assetDir.ReadLink(path)
	}
	fi, restPath := u.ParsePath(path)
	if _, ok := fi.(*UserSftpConn); ok && restPath == "" {
		return "", sftp.ErrSshFxOpUnsupported
	}

	if _, ok := fi.(*NodeDir); ok {
		return "", errNoSelectAsset
	}
	if assetDir, ok := fi.(*AssetDir); ok {
		return assetDir.ReadLink(restPath)
	}

	return "", errNoSelectAsset
}

func (u *UserSftpConn) Rename(oldNamePath, newNamePath string) (err error) {
	if u.assetDir != nil {
		return u.assetDir.Rename(oldNamePath, newNamePath)
	}
	oldFi, oldRestPath := u.ParsePath(oldNamePath)
	newFi, newRestPath := u.ParsePath(newNamePath)
	if oldAssetDir, ok := oldFi.(*AssetDir); ok {
		if newAssetDir, newOk := newFi.(*AssetDir); newOk {
			if oldAssetDir == newAssetDir {
				return oldAssetDir.Rename(oldRestPath, newRestPath)
			}
		}

	}
	return sftp.ErrSshFxOpUnsupported
}

func (u *UserSftpConn) RemoveDirectory(path string) (err error) {
	if u.assetDir != nil {
		return u.assetDir.RemoveDirectory(path)
	}
	fi, restPath := u.ParsePath(path)
	if _, ok := fi.(*UserSftpConn); ok && restPath == "" {
		return sftp.ErrSshFxPermissionDenied
	}

	if _, ok := fi.(*NodeDir); ok {
		return errNoSelectAsset
	}
	if assetDir, ok := fi.(*AssetDir); ok {
		return assetDir.RemoveDirectory(restPath)
	}
	return errNoSelectAsset
}

func (u *UserSftpConn) Remove(path string) (err error) {
	if u.assetDir != nil {
		return u.assetDir.Remove(path)
	}
	fi, restPath := u.ParsePath(path)
	if _, ok := fi.(*UserSftpConn); ok && restPath == "" {
		return sftp.ErrSshFxPermissionDenied
	}

	if _, ok := fi.(*NodeDir); ok {
		return errNoSelectAsset
	}
	if assetDir, ok := fi.(*AssetDir); ok {
		return assetDir.Remove(restPath)
	}
	return errNoSelectAsset
}

func (u *UserSftpConn) MkdirAll(path string) (err error) {
	if u.assetDir != nil {
		return u.assetDir.MkdirAll(path)
	}

	fi, restPath := u.ParsePath(path)
	if _, ok := fi.(*UserSftpConn); ok && restPath == "" {
		return sftp.ErrSshFxPermissionDenied
	}

	if _, ok := fi.(*NodeDir); ok {
		return errNoSelectAsset
	}
	if assetDir, ok := fi.(*AssetDir); ok {
		return assetDir.MkdirAll(restPath)
	}
	return errNoSelectAsset
}

func (u *UserSftpConn) Symlink(oldNamePath, newNamePath string) (err error) {
	if u.assetDir != nil {
		return u.assetDir.Symlink(oldNamePath, newNamePath)
	}
	oldFi, oldRestPath := u.ParsePath(oldNamePath)
	newFi, newRestPath := u.ParsePath(newNamePath)
	if oldAssetDir, ok := oldFi.(*AssetDir); ok {
		if newAssetDir, newOk := newFi.(*AssetDir); newOk {
			if oldAssetDir == newAssetDir {
				return oldAssetDir.Symlink(oldRestPath, newRestPath)
			}
		}
	}
	return sftp.ErrSshFxPermissionDenied
}

func (u *UserSftpConn) Create(path string) (*SftpFile, error) {
	if u.assetDir != nil {
		return u.assetDir.Create(path)
	}

	fi, restPath := u.ParsePath(path)
	if _, ok := fi.(*UserSftpConn); ok {
		return nil, sftp.ErrSshFxPermissionDenied
	}

	if _, ok := fi.(*NodeDir); ok {
		return nil, errNoSelectAsset
	}
	if assetDir, ok := fi.(*AssetDir); ok {
		return assetDir.Create(restPath)
	}

	return nil, errNoSelectAsset
}

func (u *UserSftpConn) Open(path string) (*SftpFile, error) {
	if u.assetDir != nil {
		return u.assetDir.Open(path)
	}
	fi, restPath := u.ParsePath(path)
	if _, ok := fi.(*UserSftpConn); ok {
		return nil, sftp.ErrSshFxPermissionDenied
	}

	if _, ok := fi.(*NodeDir); ok {
		return nil, errNoSelectAsset
	}
	if assetDir, ok := fi.(*AssetDir); ok {
		return assetDir.Open(restPath)
	}

	return nil, errNoSelectAsset
}

func (u *UserSftpConn) Close() {
	for _, dir := range u.Dirs {
		if nodeDir, ok := dir.(*NodeDir); ok {
			nodeDir.close()
			continue
		}
		if assetDir, ok := dir.(*AssetDir); ok {
			assetDir.close()
			continue
		}
	}
	if u.searchDir != nil {
		u.searchDir.close()
	}
	close(u.closed)
}

func (u *UserSftpConn) Name() string {
	return "/"
}

func (u *UserSftpConn) Size() int64 { return 0 }

func (u *UserSftpConn) Mode() os.FileMode {
	return os.FileMode(0444) | os.ModeDir
}

func (u *UserSftpConn) ModTime() time.Time { return u.modeTime }

func (u *UserSftpConn) IsDir() bool { return true }

func (u *UserSftpConn) Sys() interface{} {
	return &syscall.Stat_t{Uid: 0, Gid: 0}
}

func (u *UserSftpConn) List() (res []os.FileInfo, err error) {
	if u.assetDir != nil {
		return u.assetDir.ReadDir("/")
	}
	for _, item := range u.Dirs {
		res = append(res, item)
	}
	return
}

func (u *UserSftpConn) ParsePath(path string) (fi os.FileInfo, restPath string) {
	path = strings.TrimPrefix(path, "/")
	data := strings.Split(path, "/")
	if len(data) == 1 && data[0] == "" {
		fi = u
		return
	}
	var dirs map[string]os.FileInfo
	var ok bool

	if data[0] == SearchFolderName {
		dirs = u.searchDir.subDirs
		data = data[1:]
	} else {
		dirs = u.Dirs
	}
	for i := 0; i < len(data); i++ {
		fi, ok = dirs[data[i]]
		if !ok {
			restPath = strings.Join(data[i+1:], "/")
			break
		}
		if nodeDir, ok := fi.(*NodeDir); ok {
			nodeDir.loadSubNodeTree()
			dirs = nodeDir.subDirs
			continue
		}
		if assetDir, ok := fi.(*AssetDir); ok {
			assetDir.loadSystemUsers()
			restPath = strings.Join(data[i+1:], "/")
			break
		}
	}
	return
}

func (u *UserSftpConn) generateSubFoldersFromRootTree() map[string]os.FileInfo {
	nodeTrees, err := u.jmsService.GetNodeTreeByUserAndNodeKey(u.User.ID, "")
	if err != nil {
		logger.Errorf("User sftp initial err: %s", err)
		return map[string]os.FileInfo{}
	}
	u.searchDir = &SearchResultDir{
		folderName: SearchFolderName,
		modeTime:   time.Now().UTC(),
		subDirs:    map[string]os.FileInfo{}}
	return u.generateSubFoldersFromNodeTree(nodeTrees, true)
}

func (u *UserSftpConn) LoadNodeSubFoldersByKey(nodeKey string) SubFoldersLoadFunc {
	return func() map[string]os.FileInfo {
		nodeTrees, err := u.jmsService.GetNodeTreeByUserAndNodeKey(u.User.ID, nodeKey)
		if err != nil {
			logger.Error(err)
			return nil
		}
		return u.generateSubFoldersFromNodeTree(nodeTrees, false)
	}
}

func (u *UserSftpConn) generateSubFoldersFromNodeTree(nodeTrees model.NodeTreeList, isRoot bool) map[string]os.FileInfo {
	dirs := map[string]os.FileInfo{}
	matchFunc := func(s string) bool {
		_, ok := dirs[s]
		return ok
	}
	for _, item := range nodeTrees {
		if isRoot && item.Pid != "" {
			// 根路径下目录 pid 是空字符
			continue
		}
		if item.ChkDisabled {
			// 资产被禁用，不显示
			continue
		}
		switch item.Meta.Type {
		case model.TreeTypeNode:
			node := item.Meta.Data
			folderName := cleanFolderName(node.Value)
			folderName = findAvailableKeyByPaddingSuffix(matchFunc, folderName, paddingCharacter)
			loadFunc := u.LoadNodeSubFoldersByKey(node.Key)
			opts := make([]FolderBuilderOption, 0, 3)
			opts = append(opts, WithFolderID(item.ID))
			opts = append(opts, WithFolderName(folderName))
			opts = append(opts, WithSubFoldersLoadFunc(loadFunc))
			nodeDir := NewNodeDir(opts...)
			dirs[folderName] = &nodeDir
		case model.TreeTypeAsset:
			assetMeta := item.Meta.Data
			if !assetMeta.SupportSFTP {
				logger.Debugf("Asset %s not support sftp protocol ignore", item.Name)
				continue
			}
			folderName := cleanFolderName(item.Name)
			folderName = findAvailableKeyByPaddingSuffix(matchFunc, folderName, paddingCharacter)
			opts := make([]FolderBuilderOption, 0, 4)
			opts = append(opts, WithFolderID(item.ID))
			opts = append(opts, WithFolderName(folderName))
			opts = append(opts, WitRemoteAddr(u.Addr))
			opts = append(opts, WithFromType(u.loginFrom))
			opts = append(opts, WithTerminalConfig(u.opts.terminalCfg))
			assetDir := NewAssetDir(u.jmsService, u.User, opts...)
			dirs[folderName] = assetDir
		}
	}
	return dirs
}

func (u *UserSftpConn) generateSubFoldersFromToken(token *model.ConnectToken) map[string]os.FileInfo {
	dirs := make(map[string]os.FileInfo)
	asset := token.Asset
	if !asset.IsSupportProtocol(ProtocolSFTP) {
		return dirs
	}
	folderName := cleanFolderName(asset.Name)
	opts := make([]FolderBuilderOption, 0, 5)
	opts = append(opts, WithFolderID(asset.ID))
	opts = append(opts, WithFolderName(folderName))
	opts = append(opts, WitRemoteAddr(u.Addr))
	opts = append(opts, WithToken(token))
	opts = append(opts, WithFromType(u.loginFrom))
	opts = append(opts, WithTerminalConfig(u.opts.terminalCfg))
	assetDir := NewAssetDir(u.jmsService, u.User, opts...)
	assetDir.loadSystemUsers()
	dirs[folderName] = assetDir
	u.assetDir = assetDir
	return dirs
}

func (u *UserSftpConn) generateSubFoldersFromAssets(assets []model.PermAsset) map[string]os.FileInfo {
	dirs := make(map[string]os.FileInfo)
	matchFunc := func(s string) bool {
		_, ok := dirs[s]
		return ok
	}
	for i := range assets {
		// todo: 后期优化 API 循环查询的情况
		permAssetDetail, err := u.jmsService.GetUserPermAssetDetailById(u.User.ID, assets[i].ID)
		if err != nil {
			logger.Errorf("Get perm detail asset %s err: %s", assets[i].Name, err)
			continue
		}
		if !permAssetDetail.SupportProtocol(ProtocolSFTP) {
			continue
		}
		folderName := cleanFolderName(assets[i].Name)
		folderName = findAvailableKeyByPaddingSuffix(matchFunc, folderName, paddingCharacter)
		opts := make([]FolderBuilderOption, 0, 5)
		opts = append(opts, WithFolderID(assets[i].ID))
		opts = append(opts, WithFolderName(folderName))
		opts = append(opts, WitRemoteAddr(u.Addr))
		opts = append(opts, WithAsset(assets[i]))
		opts = append(opts, WithFromType(u.loginFrom))
		opts = append(opts, WithTerminalConfig(u.opts.terminalCfg))
		opts = append(opts, WithFolderUsername(u.opts.accountUsername))
		assetDir := NewAssetDir(u.jmsService, u.User, opts...)
		dirs[folderName] = assetDir
	}
	return dirs
}

func (u *UserSftpConn) Search(key string) (res []os.FileInfo, err error) {
	if u.searchDir == nil {
		logger.Error("not found search folder")
		return nil, errors.New("not found")
	}
	assets, err := u.jmsService.SearchPermAsset(u.User.ID, key)
	if err != nil {
		logger.Errorf("search asset err: %s", err)
		return nil, err
	}
	dirs := u.generateSubFoldersFromAssets(assets)
	u.searchDir.SetSubDirs(dirs)
	return u.searchDir.List()
}

type userSftpOption struct {
	user       *model.User
	RemoteAddr string
	loginFrom  model.LabelField
	assets     []model.PermAsset
	token      *model.ConnectToken

	accountUsername string

	terminalCfg *model.TerminalConfig
}

type UserSftpOption func(*userSftpOption)

func WithUser(user *model.User) UserSftpOption {
	return func(o *userSftpOption) {
		o.user = user
	}
}

func WithRemoteAddr(addr string) UserSftpOption {
	return func(o *userSftpOption) {
		o.RemoteAddr = addr
	}
}

func WithLoginFrom(loginFrom model.LabelField) UserSftpOption {
	return func(o *userSftpOption) {
		o.loginFrom = loginFrom
	}
}

func WithAssets(assets []model.PermAsset) UserSftpOption {
	return func(o *userSftpOption) {
		o.assets = assets
	}
}

func WithConnectToken(token *model.ConnectToken) UserSftpOption {
	return func(o *userSftpOption) {
		o.token = token
	}
}

func WithAccountUsername(username string) UserSftpOption {
	return func(o *userSftpOption) {
		o.accountUsername = username
	}
}

func WithTerminalCfg(cfg *model.TerminalConfig) UserSftpOption {
	return func(o *userSftpOption) {
		o.terminalCfg = cfg
	}
}

func NewUserSftpConn(jmsService *service.JMService, opts ...UserSftpOption) *UserSftpConn {
	var sftpOpts userSftpOption
	for _, setter := range opts {
		setter(&sftpOpts)
	}
	u := UserSftpConn{
		User:       sftpOpts.user,
		Addr:       sftpOpts.RemoteAddr,
		loginFrom:  sftpOpts.loginFrom,
		Dirs:       map[string]os.FileInfo{},
		modeTime:   time.Now().UTC(),
		closed:     make(chan struct{}),
		jmsService: jmsService,
		opts:       &sftpOpts,
	}

	switch {
	case sftpOpts.token != nil:
		u.Dirs = u.generateSubFoldersFromToken(sftpOpts.token)
	case len(sftpOpts.assets) > 0:
		u.Dirs = u.generateSubFoldersFromAssets(sftpOpts.assets)
	default:
		u.Dirs = u.generateSubFoldersFromRootTree()
	}
	go u.run()
	return &u
}

func (u *UserSftpConn) run() {
	tick := time.NewTicker(time.Minute)
	defer tick.Stop()
	for {
		select {
		case <-u.closed:
			logger.Infof("User %s sftp conn closed", u.User.String())
			return
		case <-tick.C:
			logger.Debugf("User %s sftp conn check expired", u.User.String())
		}
		for _, dir := range u.Dirs {
			if nodeDir, ok := dir.(*NodeDir); ok {
				nodeDir.checkExpired()
				continue
			}
			if assetDir, ok := dir.(*AssetDir); ok {
				assetDir.checkExpired()
			}
		}
	}
}

func cleanFolderName(folderName string) string {
	return strings.ReplaceAll(folderName, SFTPPathSeparator, "_")
}

const (
	SFTPPathSeparator = "/"
	paddingCharacter  = "_"
)

func findAvailableKeyByPaddingSuffix(match func(s string) bool, key string, suffix string) string {
	for match(key) {
		key = fmt.Sprintf("%s%s", key, suffix)
	}
	return key
}
