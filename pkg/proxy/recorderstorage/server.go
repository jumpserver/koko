package recorderstorage

import (
	"path/filepath"
	"strings"

	"github.com/jumpserver/koko/pkg/model"
	"github.com/jumpserver/koko/pkg/service"
)

type ServerStorage struct {
	StorageType string
}

func (s ServerStorage) BulkSave(commands []*model.Command) (err error) {
	return service.PushSessionCommand(commands)
}

func (s ServerStorage) Upload(gZipFilePath, target string) (err error) {
	sessionID := strings.Split(filepath.Base(gZipFilePath), ".")[0]
	return service.PushSessionReplay(sessionID, gZipFilePath)
}

func (s ServerStorage) TypeName() string {
	return s.StorageType
}
