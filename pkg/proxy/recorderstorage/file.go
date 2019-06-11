package recorderstorage

import (
	"fmt"
	"os"

	"github.com/jumpserver/koko/pkg/model"
)

func NewFileCommandStorage(name string) (storage *FileCommandStorage, err error) {
	file, err := os.Create(name)
	if err != nil {
		return
	}
	storage = &FileCommandStorage{File: file}
	return
}

type FileCommandStorage struct {
	File *os.File
}

func (f *FileCommandStorage) BulkSave(commands []*model.Command) (err error) {
	for _, cmd := range commands {
		f.File.WriteString(fmt.Sprintf("命令: %s\n", cmd.Input))
		f.File.WriteString(fmt.Sprintf("结果: %s\n", cmd.Output))
		f.File.WriteString("---\n")
	}
	return
}
