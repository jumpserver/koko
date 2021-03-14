package ftplogutil

import (
	"github.com/jumpserver/koko/pkg/common"
	"github.com/jumpserver/koko/pkg/config"
	"github.com/jumpserver/koko/pkg/model"
	"io"
	"os"
	"path/filepath"
)

func GetFileCacheRootPath() string {
	conf := config.GetConf()
	return filepath.Join(conf.RootPath, "data", "files")
}

func GetFileCachePath(ftpLog *model.FTPLog) (string, error) {
	directory := filepath.Join(GetFileCacheRootPath(), ftpLog.DataStart[0:10])
	if !common.FileExists(directory) {
		err := os.MkdirAll(directory, os.ModePerm)
		if err != nil {
			return directory, err
		}
	}

	fileCacheDir := filepath.Join(directory, ftpLog.Id)
	return fileCacheDir, nil
}

func CacheFileLocally(ftpLog *model.FTPLog, reader io.Reader) (string, error) {
	path, err := GetFileCachePath(ftpLog)
	if err != nil {
		return path, err
	}

	localDst, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE, 0644)
	if err != nil {
		return path, err
	}
	defer localDst.Close()
	_, err = io.Copy(localDst, reader)
	SendNotifyFileReady(*ftpLog)
	return path, err
}

func RemoveTmpChunkFile(ftpLog *model.FTPLog) error {
	path, err := GetFileCachePath(ftpLog)
	if err != nil {
		return err
	}
	pathChunk := path + "_chunk"
	if err != nil {
		return err
	}
	// 非第一次上传时，删除中间暂存文件
	_, err = os.Stat(pathChunk)
	if err == nil {
		err := os.Remove(pathChunk)
		if err != nil {
			return err
		}
	}
	return nil
}

/*大文件由于已经拆分成多块，每次调用此方法时只是文件的其中一块，所以先将reader写入暂存文件，再将暂存文件写入最终的缓存文件，再将暂存文件返回，用于上传*/
func CacheChunkFileLocally(ftpLog *model.FTPLog, reader io.Reader) (string, error) {
	path, err := GetFileCachePath(ftpLog)
	if err != nil {
		return path, err
	}
	tmpChunkPath := path + "_chunk"

	// 非第一次上传时，中间暂存文件已存在，需先删除
	err = RemoveTmpChunkFile(ftpLog)
	if err != nil {
		return tmpChunkPath, err
	}

	// 把文件写入临时文件
	localChunkDst, err := os.OpenFile(tmpChunkPath, os.O_WRONLY|os.O_CREATE, 0644)
	if err != nil {
		return tmpChunkPath, err
	}
	_, err = io.Copy(localChunkDst, reader)
	if err != nil {
		return tmpChunkPath, err
	}
	localChunkDst.Close()

	localChunkReader, err := os.Open(tmpChunkPath)
	if err != nil {
		return tmpChunkPath, err
	}
	defer localChunkReader.Close()

	// 把文件追加写入缓存文件
	path, err = GetFileCachePath(ftpLog)
	if err != nil {
		return path, err
	}
	localDst, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0644)
	if err != nil {
		return path, err
	}
	defer localDst.Close()
	_, err = io.Copy(localDst, localChunkReader)

	return tmpChunkPath, err
}
