package proxy

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/jumpserver/koko/pkg/config"
	"github.com/jumpserver/koko/pkg/logger"

	"github.com/jumpserver-dev/sdk-go/common"
	"github.com/jumpserver-dev/sdk-go/model"
	"github.com/jumpserver-dev/sdk-go/service"
	"github.com/jumpserver-dev/sdk-go/storage"
)

const (
	dateTimeFormat = "2006-01-02"
)

type FTPFileRecorder struct {
	jmsService *service.JMService
	storage    storage.FTPFileStorage

	TargetPrefix string

	MaxStoreFileSize int64

	ftpLogMap map[string]*FTPFileInfo

	lock sync.RWMutex
}

func (r *FTPFileRecorder) removeFTPFile(id string) {
	r.lock.Lock()
	defer r.lock.Unlock()
	delete(r.ftpLogMap, id)
}

func (r *FTPFileRecorder) getFTPFile(id string) *FTPFileInfo {
	r.lock.RLock()
	defer r.lock.RUnlock()
	return r.ftpLogMap[id]
}

func (r *FTPFileRecorder) setFTPFile(id string, info *FTPFileInfo) {
	r.lock.Lock()
	defer r.lock.Unlock()
	r.ftpLogMap[id] = info
}

func (r *FTPFileRecorder) CreateFTPFileInfo(logData *model.FTPLog) (info *FTPFileInfo, err error) {
	info = &FTPFileInfo{
		ftpLog: logData,

		maxWrittenSize: r.MaxStoreFileSize,
	}
	today := info.ftpLog.DateStart.UTC().Format(dateTimeFormat)
	ftpFileRootDir := config.GetConf().FTPFilePath
	ftpFileDirPath := filepath.Join(ftpFileRootDir, today)
	err = config.EnsureDirExist(ftpFileDirPath)
	if err != nil {
		logger.Errorf("Create dir %s error: %s\n", ftpFileDirPath, err)
		return nil, err
	}
	absFilePath := filepath.Join(ftpFileDirPath, logData.ID)
	storageTargetName := strings.Join([]string{FTPTargetPrefix, today, logData.ID}, "/")
	info.absFilePath = absFilePath
	info.Target = storageTargetName
	fd, err := os.OpenFile(info.absFilePath, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0644)
	if err != nil {
		logger.Errorf("Create FTP file %s error: %s\n", absFilePath, err)
		return nil, err
	}
	logger.Debugf("Create or open FTP file %s", absFilePath)
	info.fd = fd
	r.setFTPFile(logData.ID, info)
	return info, nil
}

func (r *FTPFileRecorder) Record(ftpLog *model.FTPLog, reader io.Reader) (err error) {
	if r.isNullStorage() {
		return
	}
	info := r.getFTPFile(ftpLog.ID)
	if info == nil {
		info, err = r.CreateFTPFileInfo(ftpLog)
	}
	if err != nil {
		return err
	}
	if err1 := info.WriteFromReader(reader); err1 != nil {
		logger.Errorf("FTP file %s write err: %s", ftpLog.ID, err1)
	}
	_ = info.Close()
	go r.UploadFile(3, ftpLog.ID)
	return
}

func (r *FTPFileRecorder) isNullStorage() bool {
	return r.storage.TypeName() == "null" || r.MaxStoreFileSize == 0
}

func (r *FTPFileRecorder) exceedFileMaxSize(info *FTPFileInfo) bool {
	stat, err := os.Stat(info.absFilePath)
	if err == nil {
		if stat.Size() >= r.MaxStoreFileSize {
			return true
		}
	}
	return false
}
func (r *FTPFileRecorder) UploadFile(maxRetry int, ftpLogId string) {
	if r.isNullStorage() {
		return
	}
	info := r.getFTPFile(ftpLogId)
	if info == nil {
		logger.Errorf("FTP file %s not found", ftpLogId)
		return
	}
	if !common.Have(info.absFilePath) {
		logger.Infof("FTP file not found: %s", info.absFilePath)
		return
	}
	if r.exceedFileMaxSize(info) {
		logger.Info("FTP file is exceeds the upper limit for saving files, removed: ",
			info.absFilePath)
		_ = os.Remove(info.absFilePath)
		r.removeFTPFile(info.ftpLog.ID)
		return
	}
	logger.Infof("FTPLog %s: FTP File recorder is uploading", info.ftpLog.ID)

	for i := 0; i <= maxRetry; i++ {
		logger.Infof("Upload FTP file: %s, type: %s", info.absFilePath, r.storage.TypeName())
		err := r.storage.Upload(info.absFilePath, info.Target)
		if err == nil {
			_ = os.Remove(info.absFilePath)
			if err := r.jmsService.FinishFTPFile(info.ftpLog.ID); err != nil {
				logger.Errorf("FTP file %s upload failed: %s", info.ftpLog.ID, err)
			}
			r.removeFTPFile(info.ftpLog.ID)
			break
		}
		logger.Errorf("Upload FTP file err: %s", err)
		// 如果还是失败，上传 server 再传一次
		if i == maxRetry {
			if r.storage.TypeName() == "server" {
				break
			}
			logger.Errorf("Session[%s] using server storage retry upload", info.ftpLog.ID)
			r.storage = storage.FTPServerStorage{StorageType: "server", JmsService: r.jmsService}
			r.UploadFile(3, info.ftpLog.ID)
			break
		}
	}
}

func (r *FTPFileRecorder) RecordWrite(ftpLog *model.FTPLog, p []byte) (err error) {
	if r.isNullStorage() {
		return
	}
	info := r.getFTPFile(ftpLog.ID)
	if info == nil {
		info, err = r.CreateFTPFileInfo(ftpLog)
	}
	if err != nil {
		logger.Errorf("FTP file %s create err: %s", ftpLog.ID, err)
		return
	}
	if info.isExceedWrittenSize() {
		logger.Errorf("FTP file %s is exceeds the max limit and discard it", ftpLog.ID)
		return nil
	}
	return info.WriteChunk(p)
}

func (r *FTPFileRecorder) FinishFTPFile(id string) {
	info := r.getFTPFile(id)
	if info == nil {
		return
	}
	_ = info.Close()
	go r.UploadFile(3, id)
}

func (r *FTPFileRecorder) RemoveFtpLog(id string) {
	if r.isNullStorage() {
		return
	}
	info := r.getFTPFile(id)
	if info == nil {
		return
	}
	_ = info.Close()
	_ = os.Remove(info.absFilePath)
	r.removeFTPFile(id)
}

func NewFTPFileRecord(jmsService *service.JMService, storage storage.FTPFileStorage, maxStore int64) *FTPFileRecorder {
	recorder := &FTPFileRecorder{
		jmsService:       jmsService,
		storage:          storage,
		TargetPrefix:     FTPTargetPrefix,
		MaxStoreFileSize: maxStore,
		ftpLogMap:        make(map[string]*FTPFileInfo),
	}
	return recorder
}

const FTPTargetPrefix = "FTP_FILES"

func GetFTPFileRecorder(jmsService *service.JMService) *FTPFileRecorder {
	terminalConfig, _ := jmsService.GetTerminalConfig()
	maxStoreSize := terminalConfig.MaxStoreFTPFileSize
	maxSize := int64(maxStoreSize) * 1024 * 1024
	ftpStorage := storage.NewFTPFileStorage(jmsService, terminalConfig.ReplayStorage)
	recorder := NewFTPFileRecord(jmsService, ftpStorage, maxSize)
	return recorder
}

type FTPFileInfo struct {
	ftpLog *model.FTPLog
	fd     *os.File

	absFilePath string
	Target      string

	maxWrittenSize int64
	writtenBytes   int64
}

func (f *FTPFileInfo) WriteChunk(p []byte) error {
	nw, err := f.fd.Write(p)
	if nw > 0 {
		f.writtenBytes += int64(nw)
	}
	return err

}

func (f *FTPFileInfo) WriteFromReader(r io.Reader) error {
	buf := make([]byte, 32*1024)
	var err error
	for {
		nr, er := r.Read(buf)
		if nr > 0 {
			nw, ew := f.fd.Write(buf[0:nr])
			if nw > 0 {
				f.writtenBytes += int64(nw)
				if f.isExceedWrittenSize() {
					logger.Errorf("FTP file %s is exceeds the max limit and discard it",
						f.ftpLog.ID)
					return nil
				}
			}
			if ew != nil {
				err = ew
				break
			}
			if nr != nw {
				err = io.ErrShortWrite
				break
			}
		}
		if er != nil {
			if er != io.EOF {
				err = er
			}
			break
		}
	}
	return err
}

func (f *FTPFileInfo) isExceedWrittenSize() bool {
	return f.writtenBytes >= f.maxWrittenSize
}

func (f *FTPFileInfo) Close() error {
	if f.fd != nil {
		err := f.fd.Close()
		f.fd = nil
		return err
	}
	return nil
}
