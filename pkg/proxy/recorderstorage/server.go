package recorderstorage

import (
	"path/filepath"
	"strings"

	"github.com/jumpserver/koko/pkg/model"
	"github.com/jumpserver/koko/pkg/service"
)

type ServerCommandStorage struct {
}

func (s ServerCommandStorage) BulkSave(commands []*model.Command) (err error) {
	return service.PushSessionCommand(commands)
}

type ServerReplayStorage struct {
	StorageType string
}

func (s ServerReplayStorage) Upload(gZipFilePath, target string) (err error) {
	sessionID := strings.Split(filepath.Base(gZipFilePath), ".")[0]
	return service.PushSessionReplay(sessionID, gZipFilePath)
}
