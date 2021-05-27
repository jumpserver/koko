package proxy

import (
	"strings"

	"github.com/jumpserver/koko/pkg/jms-sdk-go/model"
	"github.com/jumpserver/koko/pkg/jms-sdk-go/service"
	storage "github.com/jumpserver/koko/pkg/proxy/recorderstorage"
)

type StorageType interface {
	TypeName() string
}

type ReplayStorage interface {
	Upload(gZipFile, target string) error
	StorageType
}

type CommandStorage interface {
	BulkSave(commands []*model.Command) error
	StorageType
}

func NewReplayStorage(jmsService *service.JMService, conf *model.TerminalConfig) ReplayStorage {
	cf := conf.ReplayStorage
	tp, ok := cf["TYPE"]
	if !ok {
		tp = "server"
	}
	switch tp {
	case "azure":
		var accountName string
		var accountKey string
		var containerName string
		var endpointSuffix string
		if value, ok := cf["ENDPOINT_SUFFIX"].(string); ok {
			endpointSuffix = value
		}
		if value, ok := cf["ACCOUNT_NAME"].(string); ok {
			accountName = value
		}
		if value, ok := cf["ACCOUNT_KEY"].(string); ok {
			accountKey = value
		}
		if value, ok := cf["CONTAINER_NAME"].(string); ok {
			containerName = value
		}
		if endpointSuffix == "" {
			endpointSuffix = "core.chinacloudapi.cn"
		}
		return storage.AzureReplayStorage{
			AccountName:    accountName,
			AccountKey:     accountKey,
			ContainerName:  containerName,
			EndpointSuffix: endpointSuffix,
		}
	case "oss":
		var endpoint string
		var bucket string
		var accessKey string
		var secretKey string

		if value, ok := cf["ENDPOINT"].(string); ok {
			endpoint = value
		}
		if value, ok := cf["BUCKET"].(string); ok {
			bucket = value
		}
		if value, ok := cf["ACCESS_KEY"].(string); ok {
			accessKey = value
		}
		if value, ok := cf["SECRET_KEY"].(string); ok {
			secretKey = value
		}
		return storage.OSSReplayStorage{
			Endpoint:  endpoint,
			Bucket:    bucket,
			AccessKey: accessKey,
			SecretKey: secretKey,
		}
	case "s3", "swift":
		var region string
		var endpoint string
		var bucket string
		var accessKey string
		var secretKey string
		if value, ok := cf["BUCKET"].(string); ok {
			bucket = value
		}
		if value, ok := cf["ENDPOINT"].(string); ok {
			endpoint = value
		}
		if value, ok := cf["REGION"].(string); ok {
			region = value
		}
		if value, ok := cf["ACCESS_KEY"].(string); ok {
			accessKey = value
		}
		if value, ok := cf["SECRET_KEY"].(string); ok {
			secretKey = value
		}
		if region == "" && endpoint != "" {
			endpointArray := strings.Split(endpoint, ".")
			if len(endpointArray) >= 2 {
				region = endpointArray[1]
			}
		}
		if bucket == "" {
			bucket = "jumpserver"
		}
		return storage.S3ReplayStorage{
			Bucket:    bucket,
			Region:    region,
			AccessKey: accessKey,
			SecretKey: secretKey,
			Endpoint:  endpoint,
		}
	case "obs":
		var endpoint string
		var bucket string
		var accessKey string
		var secretKey string

		if value, ok := cf["ENDPOINT"].(string); ok {
			endpoint = value
		}
		if value, ok := cf["BUCKET"].(string); ok {
			bucket = value
		}
		if value, ok := cf["ACCESS_KEY"].(string); ok {
			accessKey = value
		}
		if value, ok := cf["SECRET_KEY"].(string); ok {
			secretKey = value
		}
		return storage.OBSReplayStorage{
			Endpoint:  endpoint,
			Bucket:    bucket,
			AccessKey: accessKey,
			SecretKey: secretKey,
		}
	case "null":
		return storage.NewNullStorage()
	default:
		return storage.ServerStorage{StorageType: "server", JmsService: jmsService}
	}
}

func NewCommandStorage(jmsService *service.JMService, conf *model.TerminalConfig) CommandStorage {
	cf := conf.CommandStorage
	tp, ok := cf["TYPE"]
	if !ok {
		tp = "server"
	}
	/*
		{
		'DOC_TYPE': 'command',
		  'HOSTS': ['http://172.16.10.122:9200'],
		  'INDEX': 'jumpserver',
		  'OTHER': {'IGNORE_VERIFY_CERTS': True},
		  'TYPE': 'es'
		}
	*/
	switch tp {
	case "es", "elasticsearch":
		var hosts = make([]string, len(cf["HOSTS"].([]interface{})))
		for i, item := range cf["HOSTS"].([]interface{}) {
			hosts[i] = item.(string)
		}
		var skipVerify bool
		index := cf["INDEX"].(string)
		docType := cf["DOC_TYPE"].(string)
		if otherMap, ok := cf["OTHER"].(map[string]interface{}); ok {
			if insecureSkipVerify, ok := otherMap["IGNORE_VERIFY_CERTS"]; ok {
				skipVerify = insecureSkipVerify.(bool)
			}
		}
		if index == "" {
			index = "jumpserver"
		}
		if docType == "" {
			docType = "command"
		}
		return storage.ESCommandStorage{
			Hosts:              hosts,
			Index:              index,
			DocType:            docType,
			InsecureSkipVerify: skipVerify,
		}
	case "null":
		return storage.NewNullStorage()
	default:
		return storage.ServerStorage{StorageType: "server", JmsService: jmsService}
	}
}
