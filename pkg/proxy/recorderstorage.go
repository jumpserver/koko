package proxy

import (
	"cocogo/pkg/config"
	"cocogo/pkg/model"
	"cocogo/pkg/service"
)

type ReplayStorage interface {
	Upload(gZipFile, target string) error
}

type CommandStorage interface {
	BulkSave(commands []*model.Command) error
}

func NewReplayStorage() ReplayStorage {
	cf := config.Conf.ReplayStorage
	tp, ok := cf["TYPE"]
	if !ok {
		tp = "server"
	}
	switch tp {
	default:
		return &ServerReplayStorage{}
	}
}

func NewCommandStorage() CommandStorage {
	cf := config.Conf.CommandStorage
	tp, ok := cf["TYPE"]
	if !ok {
		tp = "server"
	}
	switch tp {
	default:
		return &ServerCommandStorage{}
	}
}

type ServerCommandStorage struct {
}

func (s *ServerCommandStorage) BulkSave(commands []*model.Command) (err error) {
	return service.PushSessionCommand(commands)
}

type ServerReplayStorage struct {
	StorageType string
}

func (s *ServerReplayStorage) Upload(gZipFilePath, target string) (err error) {
	//sessionID := strings.Split(filepath.Base(gZipFilePath), ".")[0]
	//_ = client.PushSessionReplay(gZipFilePath, sessionID)
	return
}
