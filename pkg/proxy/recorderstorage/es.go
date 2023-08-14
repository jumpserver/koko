package recorderstorage

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/elastic/go-elasticsearch/v6"

	"github.com/jumpserver/koko/pkg/jms-sdk-go/model"
	"github.com/jumpserver/koko/pkg/logger"
)

type ESCommandStorage struct {
	Hosts   []string
	Index   string
	DocType string

	IsDataStream       bool
	InsecureSkipVerify bool
}

func (es ESCommandStorage) BulkSave(commands []*model.Command) (err error) {
	var buf bytes.Buffer
	transport := http.DefaultTransport.(*http.Transport).Clone()
	tlsClientConfig := &tls.Config{InsecureSkipVerify: es.InsecureSkipVerify}
	transport.TLSClientConfig = tlsClientConfig
	esClient, err := elasticsearch.NewClient(elasticsearch.Config{
		Addresses: es.Hosts,
		Transport: transport,
	})
	if err != nil {
		logger.Errorf("ES new client err: %s", err)
		return err
	}
	action := "index"
	if es.IsDataStream {
		action = "create"
	}
	for _, item := range commands {
		meta := []byte(fmt.Sprintf(`{ "%s" : { } }%s`, action, "\n"))
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
	var (
		blk        *bulkResponse
		raw        map[string]interface{}
		numErrors  int64
		numIndexed int64
	)
	if response.IsError() {
		if err = json.NewDecoder(response.Body).Decode(&raw); err != nil {
			logger.Errorf("ES failure to parse response body: %s", err)
		} else {
			logger.Errorf("ES failure to bulk save: [%d] %s: %s",
				response.StatusCode, raw["error"].(map[string]interface{})["type"],
				raw["error"].(map[string]interface{})["reason"],
			)
		}
		return errors.New("es failure to bulk save")
	}

	if err = json.NewDecoder(response.Body).Decode(&blk); err != nil {
		logger.Errorf("ES failure to parse response body: %s", err)
	} else {
		for _, d := range blk.Items {
			if d[action].Status > 201 {
				numErrors++
				logger.Errorf("ES failure to save: [%d]: %s: %s: %s: %s",
					d[action].Status,
					d[action].Error.Type,
					d[action].Error.Reason,
					d[action].Error.Cause.Type,
					d[action].Error.Cause.Reason,
				)
			} else {
				numIndexed++
			}
		}
	}
	logger.Infof("ES client try bulk save %d commands: success %d failure %d",
		len(commands), numIndexed, numErrors)
	return
}

func (es ESCommandStorage) TypeName() string {
	return "es"
}

type bulkActionResponse struct {
	ID     string `json:"_id"`
	Result string `json:"result"`
	Status int    `json:"status"`
	Error  struct {
		Type   string `json:"type"`
		Reason string `json:"reason"`
		Cause  struct {
			Type   string `json:"type"`
			Reason string `json:"reason"`
		} `json:"caused_by"`
	} `json:"error"`
}

// https://www.elastic.co/guide/en/elasticsearch/reference/master/docs-bulk.html#bulk-api-response-body
type bulkResponse struct {
	Errors bool                             `json:"errors"`
	Items  []map[string]*bulkActionResponse `json:"items"`
}
