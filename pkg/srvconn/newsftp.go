package srvconn

import (
	"encoding/json"
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

type UserNewSftp struct {
	User *model.User
	Addr string
	Dirs map[string]os.FileInfo

	modeTime time.Time
	logChan  chan *model.FTPLog

	closed chan struct{}
}

func (u *UserNewSftp) ReadDir(path string) (res []os.FileInfo, err error) {
	fi, restPath := u.ParsePath(path)
	if rootDir, ok := fi.(*UserNewSftp); ok {
		return rootDir.List()
	}

	if nodeDir, ok := fi.(*NodeDir); ok {
		return nodeDir.List()
	}
	if assetDir, ok := fi.(*AssetDir); ok {
		return assetDir.ReadDir(restPath)
	}

	return nil, sftp.ErrSSHFxNoSuchFile
}

func (u *UserNewSftp) Stat(path string) (res os.FileInfo, err error) {
	fi, restPath := u.ParsePath(path)
	if rootDir, ok := fi.(*UserNewSftp); ok {
		return rootDir, nil
	}

	if nodeDir, ok := fi.(*NodeDir); ok {
		return nodeDir, nil
	}
	if assetDir, ok := fi.(*AssetDir); ok {
		return assetDir.Stat(restPath)
	}

	return nil, sftp.ErrSSHFxNoSuchFile
}

func (u *UserNewSftp) ReadLink(path string) (name string, err error) {
	fi, restPath := u.ParsePath(path)
	if _, ok := fi.(*UserNewSftp); ok && restPath == "" {
		return "", sftp.ErrSshFxOpUnsupported
	}

	if _, ok := fi.(*NodeDir); ok {
		return "", sftp.ErrSshFxOpUnsupported
	}
	if assetDir, ok := fi.(*AssetDir); ok {
		return assetDir.ReadLink(restPath)
	}

	return "", sftp.ErrSshFxOpUnsupported
}

func (u *UserNewSftp) Rename(oldNamePath, newNamePath string) (err error) {
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

func (u *UserNewSftp) RemoveDirectory(path string) (err error) {
	fi, restPath := u.ParsePath(path)
	if _, ok := fi.(*UserNewSftp); ok && restPath == "" {
		return sftp.ErrSshFxPermissionDenied
	}

	if _, ok := fi.(*NodeDir); ok {
		return sftp.ErrSshFxPermissionDenied
	}
	if assetDir, ok := fi.(*AssetDir); ok {
		return assetDir.RemoveDirectory(restPath)
	}
	return sftp.ErrSshFxPermissionDenied
}

func (u *UserNewSftp) Remove(path string) (err error) {
	fi, restPath := u.ParsePath(path)
	if _, ok := fi.(*UserNewSftp); ok && restPath == "" {
		return sftp.ErrSshFxPermissionDenied
	}

	if _, ok := fi.(*NodeDir); ok {
		return sftp.ErrSshFxPermissionDenied
	}
	if assetDir, ok := fi.(*AssetDir); ok {
		return assetDir.Remove(restPath)
	}
	return sftp.ErrSshFxPermissionDenied
}

func (u *UserNewSftp) MkdirAll(path string) (err error) {
	fi, restPath := u.ParsePath(path)
	if _, ok := fi.(*UserNewSftp); ok && restPath == "" {
		return sftp.ErrSshFxPermissionDenied
	}

	if _, ok := fi.(*NodeDir); ok {
		return sftp.ErrSshFxPermissionDenied
	}
	if assetDir, ok := fi.(*AssetDir); ok {
		return assetDir.MkdirAll(restPath)
	}
	return sftp.ErrSshFxPermissionDenied
}

func (u *UserNewSftp) Symlink(oldNamePath, newNamePath string) (err error) {
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

func (u *UserNewSftp) Create(path string) (*sftp.File, error) {
	fi, restPath := u.ParsePath(path)
	if _, ok := fi.(*UserNewSftp); ok {
		return nil, sftp.ErrSshFxPermissionDenied
	}

	if _, ok := fi.(*NodeDir); ok {
		return nil, sftp.ErrSshFxPermissionDenied
	}
	if assetDir, ok := fi.(*AssetDir); ok {
		return assetDir.Create(restPath)
	}

	return nil, sftp.ErrSshFxPermissionDenied
}

func (u *UserNewSftp) Open(path string) (*sftp.File, error) {
	fi, restPath := u.ParsePath(path)
	if _, ok := fi.(*UserNewSftp); ok {
		return nil, sftp.ErrSshFxPermissionDenied
	}

	if _, ok := fi.(*NodeDir); ok {
		return nil, sftp.ErrSshFxPermissionDenied
	}
	if assetDir, ok := fi.(*AssetDir); ok {
		return assetDir.Open(restPath)
	}

	return nil, sftp.ErrSshFxPermissionDenied
}

func (u *UserNewSftp) Close() {
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
	close(u.closed)
}

func (u *UserNewSftp) Name() string {
	return "/"
}

func (u *UserNewSftp) Size() int64 { return 0 }

func (u *UserNewSftp) Mode() os.FileMode {
	return os.ModePerm | os.ModeDir
}

func (u *UserNewSftp) ModTime() time.Time { return u.modeTime }

func (u *UserNewSftp) IsDir() bool { return true }

func (u *UserNewSftp) Sys() interface{} {
	return &syscall.Stat_t{Uid: 0, Gid: 0}
}

func (u *UserNewSftp) List() (res []os.FileInfo, err error) {
	for _, item := range u.Dirs {
		res = append(res, item)
	}
	return
}

func (u *UserNewSftp) ParsePath(path string) (fi os.FileInfo, restPath string) {
	path = strings.TrimPrefix(path, "/")
	data := strings.Split(path, "/")
	if len(data) == 1 && data[0] == "" {
		fi = u
		return
	}
	dirs := u.Dirs
	var ok bool
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

func (u *UserNewSftp) initial() {
	nodeTrees := service.GetUserNodeTreeWithAsset(u.User.ID, "", "")
	if u.Dirs == nil {
		u.Dirs = map[string]os.FileInfo{}
	}
	for _, item := range nodeTrees {
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
				logger.Errorf("convert node err: %s", err)
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
				logger.Errorf("convert asset err: %s", err)
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

func (u *UserNewSftp) LoopPushFTPLog() {
	ftpLogList := make([]*model.FTPLog, 0, 1024)
	var err error
	tick := time.NewTicker(time.Second * 20)
	defer tick.Stop()
	for {
		select {
		case <-u.closed:
			if len(ftpLogList) == 0 {
				return
			}
			data := ftpLogList[len(ftpLogList)-1]
			err = service.PushFTPLog(data)
			if err != nil {
				logger.Errorf("Create FTP log err: %s", err.Error())
			}
			ftpLogList = ftpLogList[:len(ftpLogList)-1]
		case <-tick.C:
			if len(ftpLogList) == 0 {
				continue
			}
			data := ftpLogList[len(ftpLogList)-1]
			err = service.PushFTPLog(data)
			if err == nil {
				ftpLogList = ftpLogList[:len(ftpLogList)-1]
			} else {
				logger.Errorf("Create FTP log err: %s", err.Error())
			}
		case logData, ok := <-u.logChan:
			if !ok {
				return
			}
			err = service.PushFTPLog(logData)
			if err != nil {
				logger.Errorf("Create FTP log err: %s", err.Error())
				ftpLogList = append(ftpLogList, logData)
			}
		}
	}
}

func NewUserNewSftp(user *model.User, addr string) *UserNewSftp {
	u := UserNewSftp{
		User:     user,
		Addr:     addr,
		Dirs:     map[string]os.FileInfo{},
		modeTime: time.Now().UTC(),
		logChan:  make(chan *model.FTPLog, 1024),
	}
	u.initial()
	go u.LoopPushFTPLog()
	return &u
}

func NewUserNewSftpWithAsset(user *model.User, addr string, asset model.Asset) *UserNewSftp {
	u := UserNewSftp{
		User:     user,
		Addr:     addr,
		Dirs:     map[string]os.FileInfo{},
		modeTime: time.Now().UTC(),
		logChan:  make(chan *model.FTPLog, 1024),
		closed:   make(chan struct{}),
	}
	assetDir := NewAssetDir(u.User, asset, u.Addr, u.logChan)
	u.Dirs[assetDir.folderName] = &assetDir
	go u.LoopPushFTPLog()
	return &u
}
