package recorderstorage

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/elastic/go-elasticsearch/v6"
	"github.com/elastic/go-elasticsearch/v6/esapi"
	elasticsearch8 "github.com/elastic/go-elasticsearch/v8"

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

func (es ESCommandStorage) BulkSave(commands []*model.Command) error {
	if es.IsEs8() {
		return es.BulkSaveEs8(commands)
	}
	return es.BulkSaveEs(commands)
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

func (es ESCommandStorage) bulkActionBuffer(action string, commands []*model.Command) *bytes.Buffer {
	var buf bytes.Buffer
	for _, item := range commands {
		meta := []byte(fmt.Sprintf(`{ "%s" : { "_type": "%s" } }%s`, action, es.DocType, "\n"))
		data, _ := json.Marshal(item)
		data = append(data, "\n"...)
		buf.Write(meta)
		buf.Write(data)
	}
	return &buf
}

func (es ESCommandStorage) BulkSaveEs(commands []*model.Command) error {
	action := "index"
	if es.IsDataStream {
		action = "create"
	}
	buf := es.bulkActionBuffer(action, commands)
	esClient, err := es.createEsClient()
	if err != nil {
		logger.Errorf("ES new client err: %s", err)
		return err
	}
	opts := make([]func(*esapi.BulkRequest), 0, 2)
	opts = append(opts, esClient.Bulk.WithIndex(es.Index))
	opts = append(opts, esClient.Bulk.WithDocumentType(es.DocType))
	response, err := esClient.Bulk(buf, opts...)
	if err != nil {
		logger.Errorf("ES client bulk save err: %s", err)
		return err
	}
	defer response.Body.Close()
	return es.handleResp(action, response.IsError(), response.Body)
}

func (es ESCommandStorage) BulkSaveEs8(commands []*model.Command) (err error) {
	action := "index"
	if es.IsDataStream {
		action = "create"
	}
	buf := es.bulkActionBuffer(action, commands)
	esClient, err := es.createEs8Client()
	if err != nil {
		logger.Errorf("ES8 new client err: %s", err)
		return err
	}
	response, err := esClient.Bulk(buf, esClient.Bulk.WithIndex(es.Index))
	if err != nil {
		logger.Errorf("ES8 client bulk save err: %s", err)
		return err
	}
	defer response.Body.Close()
	return es.handleResp(action, response.IsError(), response.Body)
}

func (es ESCommandStorage) handleResp(action string, isErr bool, reader io.Reader) error {
	var (
		blk        *bulkResponse
		raw        map[string]interface{}
		numErrors  int64
		numIndexed int64
	)
	if isErr {
		if err := json.NewDecoder(reader).Decode(&raw); err != nil {
			logger.Errorf("ES failure to parse response body: %s", err)
		} else {
			logger.Errorf("ES failure to bulk save: %s: %s",
				raw["error"].(map[string]interface{})["type"],
				raw["error"].(map[string]interface{})["reason"],
			)
		}
		return errors.New("es failure to bulk save")
	}

	if err := json.NewDecoder(reader).Decode(&blk); err != nil {
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
	logger.Infof("ES client bulk save commands success %d failure %d", numIndexed, numErrors)
	return nil
}

func (es ESCommandStorage) IsEs8() bool {
	esClient, err := es.createEsClient()
	if err != nil {
		return false
	}
	resp, err1 := esClient.Info()
	if err1 != nil {
		return false
	}
	defer resp.Body.Close()
	var infoResp InfoResponse
	if err2 := json.NewDecoder(resp.Body).Decode(&infoResp); err2 != nil {
		return false
	}
	return infoResp.Version.Number[0] == '8'
}

func (es ESCommandStorage) createEsClient() (*elasticsearch.Client, error) {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	tlsClientConfig := &tls.Config{InsecureSkipVerify: es.InsecureSkipVerify}
	transport.TLSClientConfig = tlsClientConfig
	cfg := elasticsearch.Config{Addresses: es.Hosts, Transport: transport}
	return elasticsearch.NewClient(cfg)
}

func (es ESCommandStorage) createEs8Client() (*elasticsearch8.Client, error) {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	tlsClientConfig := &tls.Config{InsecureSkipVerify: es.InsecureSkipVerify}
	transport.TLSClientConfig = tlsClientConfig
	cfg := elasticsearch8.Config{Addresses: es.Hosts, Transport: transport}
	return elasticsearch8.NewClient(cfg)
}

type InfoResponse struct {
	Version Version `json:"version"`
}

type Version struct {
	BuildDate string `json:"build_date"`
	Number    string `json:"number"`
}
