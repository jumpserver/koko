package srvconn

import (
	"os"
	"sync"
	"syscall"
	"time"
)

type NodeDir struct {
	ID         string
	folderName string

	subDirs  map[string]os.FileInfo
	_subDirs sync.Map

	modeTime time.Time

	once *sync.Once

	loadSubFunc SubFoldersLoadFunc
}

func (nd *NodeDir) Name() string {
	return nd.folderName
}

func (nd *NodeDir) Size() int64 { return 0 }

func (nd *NodeDir) Mode() os.FileMode {
	return os.FileMode(0444) | os.ModeDir
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

func (nd *NodeDir) loadSubNodeTree() {
	nd.once.Do(func() {
		if nd.loadSubFunc != nil {
			nd.subDirs = nd.loadSubFunc()
		}
		for k, v := range nd.subDirs {
			nd._subDirs.Store(k, v)
		}
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

func (nd *NodeDir) checkExpired() {
	nd._subDirs.Range(func(key, value interface{}) bool {
		if nodeDir, ok := value.(*NodeDir); ok {
			nodeDir.checkExpired()
			return true
		}
		if assetDir, ok := value.(*AssetDir); ok {
			assetDir.checkExpired()
		}
		return true
	})
}
