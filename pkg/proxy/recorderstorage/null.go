package recorderstorage

import (
	"github.com/jumpserver/koko/pkg/logger"
	"github.com/jumpserver/koko/pkg/model"
)

func NewNullStorage() (storage NullStorage) {
	storage = NullStorage{}
	return
}

type NullStorage struct {
}

func (f NullStorage) BulkSave(commands []*model.Command) (err error) {
	logger.Infof("Null Storage discard %d commands.", len(commands))
	return
}

func (f NullStorage) Upload(gZipFile, target string) (err error) {
	logger.Infof("Null Storage discard %s.", gZipFile)
	return
}

func (f NullStorage) TypeName() string {
	return "null"
}
