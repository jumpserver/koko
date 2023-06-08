package recorderstorage

import (
	"path/filepath"
	"strings"

	"github.com/jumpserver/koko/pkg/jms-sdk-go/model"
	"github.com/jumpserver/koko/pkg/jms-sdk-go/service"
)

type ServerStorage struct {
	StorageType string
	JmsService  *service.JMService
}

type FTPServerStorage struct {
	StorageType string
	JmsService  *service.JMService
}

func (s FTPServerStorage) Upload(filePath, target string) (err error) {
	id := strings.Split(filepath.Base(filePath), ".")[0]
	return s.JmsService.UploadFTPFile(id, filePath)
}

func (s FTPServerStorage) TypeName() string {
	return s.StorageType
}

func (s ServerStorage) BulkSave(commands []*model.Command) (err error) {
	return s.JmsService.PushSessionCommand(commands)
}

func (s ServerStorage) Upload(gZipFilePath, target string) (err error) {
	sessionID := strings.Split(filepath.Base(gZipFilePath), ".")[0]
	return s.JmsService.UploadReplay(sessionID, gZipFilePath)
}

func (s ServerStorage) TypeName() string {
	return s.StorageType
}
