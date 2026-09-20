package srvconn

import (
	"fmt"
	"os"
	"time"

	"github.com/jumpserver-dev/sdk-go/model"
	"github.com/jumpserver/koko/pkg/logger"
	"github.com/pkg/sftp"
)

// OpenUploadTemp accesses upload staging data under upload permission. Internal
// reads and writes are audited once by CommitUploadTemp, against the final path.
func (u *UserSftpConn) OpenUploadTemp(path string, create bool) (*SftpFile, error) {
	if u.assetDir != nil {
		return u.assetDir.OpenUploadTemp(path, create)
	}
	fi, restPath := u.ParsePath(path)
	if assetDir, ok := fi.(*AssetDir); ok {
		return assetDir.OpenUploadTemp(restPath, create)
	}
	return nil, sftp.ErrSshFxPermissionDenied
}

func (ad *AssetDir) OpenUploadTemp(path string, create bool) (*SftpFile, error) {
	_, con, realPath, err := ad.resolveUploadPath(path)
	if err != nil {
		return nil, err
	}
	con.IncreaseRef()
	flags := os.O_RDWR
	if create {
		// A concurrent prepare must never truncate an existing resumable upload.
		flags |= os.O_CREATE | os.O_EXCL
	}
	file, err := con.client.OpenFile(realPath, flags)
	if err != nil {
		con.DecreaseRef()
		return nil, err
	}
	return &SftpFile{File: file, cleanupFunc: con.DecreaseRef}, nil
}

func (u *UserSftpConn) CommitUploadTemp(sourcePath, targetPath string, overwrite bool) (*model.FTPLog, error) {
	if u.assetDir != nil {
		return u.assetDir.CommitUploadTemp(sourcePath, targetPath, overwrite)
	}
	sourceFi, sourceRestPath := u.ParsePath(sourcePath)
	targetFi, targetRestPath := u.ParsePath(targetPath)
	sourceAssetDir, ok := sourceFi.(*AssetDir)
	if !ok {
		return nil, sftp.ErrSshFxPermissionDenied
	}
	if targetAssetDir, targetOk := targetFi.(*AssetDir); !targetOk || targetAssetDir != sourceAssetDir {
		return nil, sftp.ErrSshFxOpUnsupported
	}
	return sourceAssetDir.CommitUploadTemp(sourceRestPath, targetRestPath, overwrite)
}

// CommitUploadTemp records the logical upload instead of a staging-file rename.
func (ad *AssetDir) CommitUploadTemp(sourcePath, targetPath string, overwrite bool) (*model.FTPLog, error) {
	_, sourceConn, sourceRealPath, err := ad.resolveUploadPath(sourcePath)
	if err != nil {
		return nil, err
	}
	su, targetConn, targetRealPath, err := ad.resolveUploadPath(targetPath)
	if err != nil {
		return nil, err
	}
	if sourceConn != targetConn {
		return nil, sftp.ErrSshFxOpUnsupported
	}
	sourceConn.IncreaseRef()
	defer sourceConn.DecreaseRef()
	if overwrite {
		err = sourceConn.client.PosixRename(sourceRealPath, targetRealPath)
	} else {
		if _, err = sourceConn.client.Stat(targetRealPath); err == nil {
			err = fmt.Errorf("file already exists")
		} else if os.IsNotExist(err) {
			err = sourceConn.client.Rename(sourceRealPath, targetRealPath)
		}
	}
	if err == nil {
		now := time.Now()
		if timeErr := sourceConn.client.Chtimes(targetRealPath, now, now); timeErr != nil {
			logger.Debugf("Set upload mtime %s failed: %s", targetRealPath, timeErr)
		}
	}
	return ad.CreateFTPLog(su, model.OperateUpload, targetRealPath, err == nil), err
}
