package recorderstorage

import (
	"path/filepath"
	"strings"

	"github.com/jumpserver/koko/pkg/jms-sdk-go/model"
	"github.com/jumpserver/koko/pkg/jms-sdk-go/service"
)

type ServerStorage struct {
	StorageType string
	FileType    string
	JmsService  *service.JMService
}

func (s ServerStorage) BulkSave(commands []*model.Command) (err error) {
	return s.JmsService.PushSessionCommand(commands)
}

func (s ServerStorage) Upload(filePath, target string) (err error) {
	id := strings.Split(filepath.Base(filePath), ".")[0]
	switch s.FileType {
	case "replay":
		return s.JmsService.UploadReplay(id, filePath)
	case "ftpFile":
		return s.JmsService.UploadFTPFile(id, filePath)
	default:
		return nil
	}
}

func (s ServerStorage) TypeName() string {
	return s.StorageType
}
