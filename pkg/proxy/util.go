package proxy

import (
	"strings"

	"github.com/jumpserver/koko/pkg/config"
	"github.com/jumpserver/koko/pkg/model"
	storage "github.com/jumpserver/koko/pkg/proxy/recorderstorage"
)

type ReplayStorage interface {
	Upload(gZipFile, target string) error
}

type CommandStorage interface {
	BulkSave(commands []*model.Command) error
}

var defaultCommandStorage = storage.ServerCommandStorage{}
var defaultReplayStorage = storage.ServerReplayStorage{StorageType: "server"}

func NewReplayStorage() ReplayStorage {
	cf := config.GetConf().ReplayStorage
	tp, ok := cf["TYPE"]
	if !ok {
		tp = "server"
	}
	switch tp {
	case "azure":
		endpointSuffix := cf["ENDPOINT_SUFFIX"].(string)
		if endpointSuffix == "" {
			endpointSuffix = "core.chinacloudapi.cn"
		}
		return storage.AzureReplayStorage{
			AccountName:    cf["ACCOUNT_NAME"].(string),
			AccountKey:     cf["ACCOUNT_KEY"].(string),
			ContainerName:  cf["CONTAINER_NAME"].(string),
			EndpointSuffix: endpointSuffix,
		}
	case "oss":
		return storage.OSSReplayStorage{
			Endpoint:  cf["ENDPOINT"].(string),
			Bucket:    cf["BUCKET"].(string),
			AccessKey: cf["ACCESS_KEY"].(string),
			SecretKey: cf["SECRET_KEY"].(string),
		}
	case "s3":
		var region string
		var endpoint string
		bucket := cf["BUCKET"].(string)
		endpoint = cf["ENDPOINT"].(string)
		if bucket == "" {
			bucket = "jumpserver"
		}
		if cf["REGION"] != nil {
			region = cf["REGION"].(string)
		} else {
			region = strings.Split(endpoint, ".")[1]
		}

		return storage.S3ReplayStorage{
			Bucket:    bucket,
			Region:    region,
			AccessKey: cf["ACCESS_KEY"].(string),
			SecretKey: cf["SECRET_KEY"].(string),
			Endpoint:  endpoint,
		}
	case "obs":
		return storage.OBSReplayStorage{
			Endpoint:  cf["ENDPOINT"].(string),
			Bucket:    cf["BUCKET"].(string),
			AccessKey: cf["ACCESS_KEY"].(string),
			SecretKey: cf["SECRET_KEY"].(string),
		}
	default:
		return defaultReplayStorage
	}
}

func NewCommandStorage() CommandStorage {
	cf := config.GetConf().CommandStorage
	tp, ok := cf["TYPE"]
	if !ok {
		tp = "server"
	}
	switch tp {
	case "es", "elasticsearch":
		var hosts = make([]string, len(cf["HOSTS"].([]interface{})))
		for i, item := range cf["HOSTS"].([]interface{}) {
			hosts[i] = item.(string)
		}
		index := cf["INDEX"].(string)
		docType := cf["DOC_TYPE"].(string)
		if index == "" {
			index = "jumpserver"
		}
		if docType == "" {
			docType = "command_store"
		}
		return storage.ESCommandStorage{Hosts: hosts, Index: index, DocType: docType}
	default:
		return defaultCommandStorage
	}
}
