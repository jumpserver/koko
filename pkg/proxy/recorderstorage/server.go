package recorderstorage

import (
	"path/filepath"
	"strings"

	"github.com/jumpserver/koko/pkg/jms-sdk-go/model"
	"github.com/jumpserver/koko/pkg/jms-sdk-go/service"
)

type BaseStorage struct {}

type ServerStorage struct {
	BaseStorage
	StorageType string
	JmsService  *service.JMService
}

type FTPServerStorage struct {
	BaseStorage
	StorageType string
	JmsService  *service.JMService
}

func (s FTPServerStorage) Upload(filePath, target string) (err error) {
	id := strings.Split(filepath.Base(filePath), ".")[0]
	return s.JmsService.UploadFTPFile(id, filePath)
}

func (s ServerStorage) BulkSave(commands []*model.Command) (err error) {
	return s.JmsService.PushSessionCommand(commands)
}

func (s ServerStorage) Upload(gZipFilePath, target string) (err error) {
	sessionID := strings.Split(filepath.Base(gZipFilePath), ".")[0]
	return s.JmsService.UploadReplay(sessionID, gZipFilePath)
}

func (s BaseStorage) TypeName() string {
	return s.StorageType
}
