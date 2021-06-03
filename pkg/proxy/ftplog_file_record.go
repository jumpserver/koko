package proxy

import (
	"fmt"
	"github.com/jumpserver/koko/pkg/ftplogutil"
	"github.com/jumpserver/koko/pkg/logger"
	"github.com/jumpserver/koko/pkg/model"
	"github.com/jumpserver/koko/pkg/service"
	"os"
	"path/filepath"
	"time"
)

func ftpLogFileRecord() {
	ftpLogMap := make(map[string]model.FTPLog)
	ftpLogFileMap := make(map[string]model.FTPLog)
	ftpLogRetryTimes := make(map[string]int)
	for {
		var ftpLogObj model.FTPLog
		select {
		case ftpLog := <-ftplogutil.LogReadyChan:
			ftpLogMap[ftpLog.Id] = ftpLog
			_, ok := ftpLogFileMap[ftpLog.Id]
			if !ok {
				continue
			}
		case ftpLog := <-ftplogutil.FileReadyChan:
			ftpLogFileMap[ftpLog.Id] = ftpLog
			_, ok := ftpLogMap[ftpLog.Id]
			if !ok {
				continue
			}
		default:
			// 只有日志和文件都ready时才上传，否则等待
			if len(ftpLogMap) == 0 || len(ftpLogFileMap) == 0 {
				time.Sleep(time.Duration(1) * time.Second)
				continue
			}
			if len(ftpLogMap) != len(ftpLogFileMap) {
				dropTimoutFtpLog(ftpLogMap, ftpLogFileMap)
			}
		}
		if ftpLogObj.Id == "" {
			for ftpLogId := range ftpLogFileMap {
				tmpFtpLog, ok := ftpLogMap[ftpLogId]
				if !ok {
					continue
				}
				ftpLogObj = tmpFtpLog
			}
		}

		// 只有日志和文件都ready时才上传，否则等待
		if len(ftpLogMap) == 0 || len(ftpLogFileMap) == 0 || ftpLogObj.Id == "" {
			time.Sleep(time.Duration(1) * time.Second)
			continue
		}

		ftpLogObjId := ftpLogObj.Id
		path, err := ftplogutil.GetFileCachePath(&ftpLogObj)
		target, _ := filepath.Rel(ftplogutil.GetFileCacheRootPath(), path)
		if err == nil && UploadFtpLogFile(path, target, ftpLogObjId) {
			delete(ftpLogMap, ftpLogObjId)
			delete(ftpLogFileMap, ftpLogObjId)
		} else {
			ftpLogRetryTimes[ftpLogObjId] += 1
			// 重试五次不成功则舍弃
			if ftpLogRetryTimes[ftpLogObjId] > 5 {
				delete(ftpLogMap, ftpLogObjId)
				delete(ftpLogFileMap, ftpLogObjId)
			}
		}
	}
}

func UploadFtpLogFile(path string, target string, ftpLogId string) (success bool) {
	targetPath := filepath.Join("FILE_STORE", target)
	logger.Infof("[Start] upload file [%s] to [%s]", path, targetPath)
	err := NewReplayStorage("file").Upload(path, targetPath)
	if err == nil {
		if service.FinishFTPLogFileUpload(ftpLogId) {
			err = os.Remove(path)
			if err != nil {
				logger.Warn(fmt.Sprintf("Delete file %s failed", path))
			}
			logger.Infof("[Success] upload file [%s] to [%s]", path, targetPath)
			return true
		}
	} else {
		logger.Errorf("[Failed] upload file [%s] to [%s], err: %s", path, targetPath, err)
	}
	return false
}

// 移除超时的ftp日志记录
func dropTimoutFtpLog(ftpLogMap map[string]model.FTPLog, ftpLogFileMap map[string]model.FTPLog) {
	for ftpLogId := range ftpLogFileMap {
		tmpFtpLog, ok := ftpLogMap[ftpLogId]

		currentTime := time.Now()
		oldTime := currentTime.AddDate(0, 0, -2)
		theTime, _ := time.Parse("2006-01-02 15:04:05 +0000", tmpFtpLog.DataStart)
		if !ok && theTime.Unix() < time.Now().Unix()-oldTime.Unix() {
			delete(ftpLogMap, tmpFtpLog.Id)
			delete(ftpLogFileMap, tmpFtpLog.Id)
		}
	}
}
