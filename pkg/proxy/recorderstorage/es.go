package recorderstorage

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/elastic/go-elasticsearch"

	"cocogo/pkg/logger"
	"cocogo/pkg/model"
)

type ESCommandStorage struct {
	Hosts   []string
	Index   string
	DocType string
}

func (es *ESCommandStorage) BulkSave(commands []*model.Command) (err error) {
	var buf bytes.Buffer
	esClinet, err := elasticsearch.NewClient(elasticsearch.Config{
		Addresses: es.Hosts,
	})
	if err != nil {
		logger.Error(err.Error())
		return
	}
	for _, item := range commands {
		meta := []byte(fmt.Sprintf(`{ "index" : { } }%s`, "\n"))
		data, err := json.Marshal(item)
		if err != nil {
			return err
		}
		data = append(data, "\n"...)
		buf.Write(meta)
		buf.Write(data)
	}

	_, err = esClinet.Bulk(bytes.NewReader(buf.Bytes()),
		esClinet.Bulk.WithIndex(es.Index), esClinet.Bulk.WithDocumentType(es.DocType))
	if err != nil {
		logger.Error(err.Error())
	}
	return
}
