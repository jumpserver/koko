package proxy

import (
	"cocogo/pkg/config"
	"cocogo/pkg/model"
	"cocogo/pkg/service"
	"fmt"
	"os"
)

type ReplayStorage interface {
	Upload(gZipFile, target string) error
}

type CommandStorage interface {
	BulkSave(commands []*model.Command) error
}

func NewReplayStorage() ReplayStorage {
	cf := config.GetConf().ReplayStorage
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
	cf := config.GetConf().CommandStorage
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

func NewFileCommandStorage(name string) (storage *FileCommandStorage, err error) {
	file, err := os.Create(name)
	if err != nil {
		return
	}
	storage = &FileCommandStorage{file: file}
	return
}

type FileCommandStorage struct {
	file *os.File
}

func (f *FileCommandStorage) BulkSave(commands []*model.Command) (err error) {
	for _, cmd := range commands {
		f.file.WriteString(fmt.Sprintf("命令: %s\n", cmd.Input))
		f.file.WriteString(fmt.Sprintf("结果: %s\n", cmd.Output))
		f.file.WriteString("---\n")
	}
	return
}

type ServerReplayStorage struct {
	StorageType string
}

func (s *ServerReplayStorage) Upload(gZipFilePath, target string) (err error) {
	//sessionID := strings.Split(filepath.Base(gZipFilePath), ".")[0]
	//_ = client.PushSessionReplay(gZipFilePath, sessionID)
	return
}
