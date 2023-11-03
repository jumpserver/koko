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

type Storage interface {
	Upload(gZipFile, target string) error
	StorageType
}

type ReplayStorage interface {
	Storage
}

type FTPFileStorage interface {
	Storage
}

type CommandStorage interface {
	BulkSave(commands []*model.Command) error
	StorageType
}

func GetStorage(conf *model.TerminalConfig) Storage {
	cfg := conf.ReplayStorage
	switch cfg.TypeName {
	case "azure":
		var (
			accountName    string
			accountKey     string
			containerName  string
			endpointSuffix string
		)
		endpointSuffix = cfg.EndpointSuffix
		accountName = cfg.AccountName
		accountKey = cfg.AccountKey
		containerName = cfg.ContainerName
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
		var (
			endpoint  string
			bucket    string
			accessKey string
			secretKey string
		)

		endpoint = cfg.Endpoint
		bucket = cfg.Bucket
		accessKey = cfg.AccessKey
		secretKey = cfg.SecretKey

		return storage.OSSReplayStorage{
			Endpoint:  endpoint,
			Bucket:    bucket,
			AccessKey: accessKey,
			SecretKey: secretKey,
		}
	case "s3", "swift", "cos":
		var (
			region    string
			endpoint  string
			bucket    string
			accessKey string
			secretKey string
		)

		bucket = cfg.Bucket
		endpoint = cfg.Endpoint
		region = cfg.Region
		accessKey = cfg.AccessKey
		secretKey = cfg.SecretKey

		if region == "" && endpoint != "" {
			region = ParseEndpointRegion(endpoint)
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
		var (
			endpoint  string
			bucket    string
			accessKey string
			secretKey string
		)

		endpoint = cfg.Endpoint
		bucket = cfg.Bucket
		accessKey = cfg.AccessKey
		secretKey = cfg.SecretKey

		return storage.OBSReplayStorage{
			Endpoint:  endpoint,
			Bucket:    bucket,
			AccessKey: accessKey,
			SecretKey: secretKey,
		}
	case "null":
		return storage.NewNullStorage()
	default:
		return nil
	}
}

func NewReplayStorage(jmsService *service.JMService, conf *model.TerminalConfig) ReplayStorage {
	replayStorage := GetStorage(conf)
	if replayStorage == nil {
		replayStorage = storage.ServerStorage{StorageType: "server", JmsService: jmsService}
	}
	return replayStorage
}

func NewFTPFileStorage(jmsService *service.JMService, conf *model.TerminalConfig) FTPFileStorage {
	ftpStorage := GetStorage(conf)
	if ftpStorage == nil {
		ftpStorage = storage.FTPServerStorage{StorageType: "server", JmsService: jmsService}
	}
	return ftpStorage
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
		  'OTHER': {'IGNORE_VERIFY_CERTS': True, 'IS_INDEX_DATASTREAM': True},
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
		var isDataStream bool
		index := cf["INDEX"].(string)
		docType := cf["DOC_TYPE"].(string)
		if otherMap, ok := cf["OTHER"].(map[string]interface{}); ok {
			if insecureSkipVerify, ok := otherMap["IGNORE_VERIFY_CERTS"]; ok {
				skipVerify = insecureSkipVerify.(bool)
			}
			if isIndexDataStream, ok := otherMap["IS_INDEX_DATASTREAM"]; ok {
				isDataStream = isIndexDataStream.(bool)
			}
		}
		if index == "" {
			index = "jumpserver"
		}
		if docType == "" {
			docType = "_doc"
		}
		return storage.ESCommandStorage{
			Hosts:              hosts,
			Index:              index,
			DocType:            docType,
			IsDataStream:       isDataStream,
			InsecureSkipVerify: skipVerify,
		}
	case "null":
		return storage.NewNullStorage()
	default:
		return storage.ServerStorage{StorageType: "server", JmsService: jmsService}
	}
}

func ParseEndpointRegion(s string) string {
	endpoints := strings.Split(s, ".")
	if len(endpoints) >= 3 {
		return endpoints[len(endpoints)-3]
	}
	return ""
}
