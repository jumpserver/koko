package recorderstorage

import (
	"github.com/jumpserver-dev/sdk-go/model"
	"github.com/jumpserver/koko/pkg/logger"
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
