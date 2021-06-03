package recorderstorage

import (
	"errors"
	"github.com/jumpserver/koko/pkg/model"
	"github.com/jumpserver/koko/pkg/service"
	"path/filepath"
	"strings"
)

type ServerStorage struct {
	StorageType string
	FileType    string
}

func (s ServerStorage) BulkSave(commands []*model.Command) (err error) {
	return service.PushSessionCommand(commands)
}

func (s ServerStorage) Upload(gZipFilePath, target string) (err error) {
	id := strings.Split(filepath.Base(gZipFilePath), ".")[0]
	switch s.FileType {
	case "replay":
		return service.PushSessionReplay(id, gZipFilePath)
	case "file":
		return service.PushFTPLogFile(id, gZipFilePath)
	default:
		return errors.New("cannot match FileType of ServerStorage")
	}
}

func (s ServerStorage) TypeName() string {
	return s.StorageType
}
