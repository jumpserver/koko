package recorderstorage

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/elastic/go-elasticsearch/v6"

	"github.com/jumpserver/koko/pkg/logger"
	"github.com/jumpserver/koko/pkg/model"
)

type ESCommandStorage struct {
	Hosts   []string
	Index   string
	DocType string
}

func (es ESCommandStorage) BulkSave(commands []*model.Command) (err error) {
	var buf bytes.Buffer
	esClient, err := elasticsearch.NewClient(elasticsearch.Config{
		Addresses: es.Hosts,
	})
	if err != nil {
		logger.Errorf("ES new client err: %s", err)
		return err
	}
	for _, item := range commands {
		meta := []byte(fmt.Sprintf(`{ "index" : { } }%s`, "\n"))
		data, err := json.Marshal(item)
		if err != nil {
			logger.Errorf("ES marshal data to json err: %s", err)
			return err
		}
		data = append(data, "\n"...)
		buf.Write(meta)
		buf.Write(data)
	}

	response, err := esClient.Bulk(bytes.NewReader(buf.Bytes()),
		esClient.Bulk.WithIndex(es.Index), esClient.Bulk.WithDocumentType(es.DocType))
	if err != nil {
		logger.Errorf("ES client bulk save err: %s", err)
		return err
	}
	defer response.Body.Close()
	logger.Debugf("ES client bulk save %d commands", len(commands))
	return
}

func (es ESCommandStorage) TypeName() string {
	return "es"
}
