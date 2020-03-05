package srvconn

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"syscall"
	"time"

	"github.com/pkg/sftp"

	"github.com/jumpserver/koko/pkg/logger"
	"github.com/jumpserver/koko/pkg/model"
	"github.com/jumpserver/koko/pkg/service"
)

var errNoSelectAsset = errors.New("please select one of the assets")

type UserSftpConn struct {
	User *model.User
	Addr string
	Dirs map[string]os.FileInfo

	modeTime time.Time
	logChan  chan *model.FTPLog

	closed chan struct{}

	searchDir *SearchResultDir
}

func (u *UserSftpConn) ReadDir(path string) (res []os.FileInfo, err error) {
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

func (u *UserSftpConn) Create(path string) (*sftp.File, error) {
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

func (u *UserSftpConn) Open(path string) (*sftp.File, error) {
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
	return os.ModePerm | os.ModeDir
}

func (u *UserSftpConn) ModTime() time.Time { return u.modeTime }

func (u *UserSftpConn) IsDir() bool { return true }

func (u *UserSftpConn) Sys() interface{} {
	return &syscall.Stat_t{Uid: 0, Gid: 0}
}

func (u *UserSftpConn) List() (res []os.FileInfo, err error) {
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
			nodeDir.loadNodeAsset(u)
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

func (u *UserSftpConn) initial() {
	nodeTrees := service.GetUserNodeTreeWithAsset(u.User.ID, "", "")
	if u.Dirs == nil {
		u.Dirs = map[string]os.FileInfo{}
	}
	u.searchDir = &SearchResultDir{
		folderName: SearchFolderName,
		modeTime:   time.Now().UTC(),
		subDirs:    map[string]os.FileInfo{}}

	for _, item := range nodeTrees {
		if item.Pid != "" {
			continue
		}
		typeName, ok := item.Meta["type"].(string)
		if !ok {
			continue
		}
		body, err := json.Marshal(item.Meta[typeName])
		if err != nil {
			logger.Errorf("Json Marshal err: %s", err)
			continue
		}
		switch typeName {
		case "node":
			node, err := model.ConvertMetaToNode(body)
			if err != nil {
				logger.Errorf("convert to node err: %s", err)
				continue
			}
			nodeDir := NewNodeDir(node)
			folderName := nodeDir.folderName
			for {
				_, ok := u.Dirs[folderName]
				if !ok {
					break
				}
				folderName = fmt.Sprintf("%s_", folderName)
			}
			if folderName != nodeDir.folderName {
				nodeDir.folderName = folderName
			}

			u.Dirs[folderName] = &nodeDir
		case "asset":
			asset, err := model.ConvertMetaToAsset(body)
			if err != nil {
				logger.Errorf("convert to asset err: %s", err)
				continue
			}
			if !asset.IsSupportProtocol("ssh") {
				continue
			}
			assetDir := NewAssetDir(u.User, asset, u.Addr, u.logChan)
			folderName := assetDir.folderName
			for {
				_, ok := u.Dirs[folderName]
				if !ok {
					break
				}
				folderName = fmt.Sprintf("%s_", folderName)
			}
			if folderName != assetDir.folderName {
				assetDir.folderName = folderName
			}
			u.Dirs[folderName] = &assetDir
		}
	}

}

func (u *UserSftpConn) loopPushFTPLog() {
	ftpLogList := make([]*model.FTPLog, 0, 1024)
	maxRetry := 0
	var err error
	tick := time.NewTicker(time.Second * 20)
	defer tick.Stop()
	for {
		select {
		case <-u.closed:
			if len(ftpLogList) == 0 {
				return
			}
		case <-tick.C:
			if len(ftpLogList) == 0 {
				continue
			}
		case logData, ok := <-u.logChan:
			if !ok {
				return
			}
			ftpLogList = append(ftpLogList, logData)
		}

		data := ftpLogList[len(ftpLogList)-1]
		err = service.PushFTPLog(data)
		if err == nil {
			ftpLogList = ftpLogList[:len(ftpLogList)-1]
			maxRetry = 0
			continue
		} else {
			logger.Errorf("Create FTP log err: %s", err.Error())
		}

		if maxRetry > 5 {
			ftpLogList = ftpLogList[1:]
		}
		maxRetry++
	}
}

func (u *UserSftpConn) Search(key string) (res []os.FileInfo, err error) {
	if u.searchDir == nil {
		logger.Errorf("not found search folder")
		return nil, fmt.Errorf("not found")
	}
	assetsTree, err := service.SearchPermAsset(u.User.ID, key)
	if err != nil {
		logger.Errorf("search asset err: %s", err)
		return nil, err
	}
	subDirs := map[string]os.FileInfo{}
	for _, item := range assetsTree {
		typeName, ok := item.Meta["type"].(string)
		if !ok {
			continue
		}
		body, err := json.Marshal(item.Meta[typeName])
		if err != nil {
			logger.Errorf("Search Json Marshal err: %s", err)
			continue
		}
		switch typeName {
		case "asset":
			asset, err := model.ConvertMetaToAsset(body)
			if err != nil {
				logger.Errorf("convert to asset err: %s", err)
				continue
			}
			if !asset.IsSupportProtocol("ssh") {
				continue
			}
			assetDir := NewAssetDir(u.User, asset, u.Addr, u.logChan)
			folderName := assetDir.folderName
			for {
				_, ok := subDirs[folderName]
				if !ok {
					break
				}
				folderName = fmt.Sprintf("%s_", folderName)
			}
			if folderName != assetDir.folderName {
				assetDir.folderName = folderName
			}
			subDirs[assetDir.folderName] = &assetDir
		}
	}
	u.searchDir.SetSubDirs(subDirs)
	return u.searchDir.List()
}

func NewUserSftpConn(user *model.User, addr string) *UserSftpConn {
	u := UserSftpConn{
		User:     user,
		Addr:     addr,
		Dirs:     map[string]os.FileInfo{},
		modeTime: time.Now().UTC(),
		logChan:  make(chan *model.FTPLog, 1024),
		closed:   make(chan struct{}),
	}
	u.initial()
	go u.loopPushFTPLog()
	return &u
}

func NewUserSftpConnWithAssets(user *model.User, addr string, assets ...model.Asset) *UserSftpConn {
	u := UserSftpConn{
		User:     user,
		Addr:     addr,
		Dirs:     map[string]os.FileInfo{},
		modeTime: time.Now().UTC(),
		logChan:  make(chan *model.FTPLog, 1024),
		closed:   make(chan struct{}),
	}
	for _, asset := range assets {
		if asset.IsSupportProtocol("ssh") {
			assetDir := NewAssetDir(u.User, asset, u.Addr, u.logChan)
			folderName := assetDir.folderName
			for {
				_, ok := u.Dirs[folderName]
				if !ok {
					break
				}
				folderName = fmt.Sprintf("%s_", folderName)
			}
			if folderName != assetDir.folderName {
				assetDir.folderName = folderName
			}
			u.Dirs[assetDir.folderName] = &assetDir
		}
	}
	go u.loopPushFTPLog()
	return &u
}
