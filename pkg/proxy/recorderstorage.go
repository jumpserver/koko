package proxy

import (
	"bytes"
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/Azure/azure-storage-blob-go/azblob"
	"github.com/aliyun/aliyun-oss-go-sdk/oss"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"
	"github.com/elastic/go-elasticsearch"

	"cocogo/pkg/config"
	"cocogo/pkg/logger"
	"cocogo/pkg/model"
	"cocogo/pkg/service"
	"encoding/json"
)

type ReplayStorage interface {
	Upload(gZipFile, target string) error
}

type CommandStorage interface {
	BulkSave(commands []*model.Command) error
}

var defaultCommandStorage = &ServerCommandStorage{}
var defaultReplayStorage = &ServerReplayStorage{StorageType: "server"}

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
		return &AzureReplayStorage{
			accountName:    cf["ACCOUNT_NAME"].(string),
			accountKey:     cf["ACCOUNT_KEY"].(string),
			containerName:  cf["CONTAINER_NAME"].(string),
			endpointSuffix: endpointSuffix,
		}
	case "oss":
		return &OSSReplayStorage{
			endpoint:  cf["ENDPOINT"].(string),
			bucket:    cf["BUCKET"].(string),
			accessKey: cf["ACCESS_KEY"].(string),
			secretKey: cf["SECRET_KEY"].(string),
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

		return &S3ReplayStorage{
			bucket:    bucket,
			region:    region,
			accessKey: cf["ACCESS_KEY"].(string),
			secretKey: cf["SECRET_KEY"].(string),
			endpoint:  endpoint,
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
		return &ESCommandStorage{hosts: hosts, index: index, docType: docType}
	default:
		return defaultCommandStorage
	}
}

type ServerCommandStorage struct {
}

func (s *ServerCommandStorage) BulkSave(commands []*model.Command) (err error) {
	return service.PushSessionCommand(commands)
}

type ESCommandStorage struct {
	hosts   []string
	index   string
	docType string
}

func (es *ESCommandStorage) BulkSave(commands []*model.Command) (err error) {
	var buf bytes.Buffer
	esClinet, err := elasticsearch.NewClient(elasticsearch.Config{
		Addresses: es.hosts,
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
		esClinet.Bulk.WithIndex(es.index), esClinet.Bulk.WithDocumentType(es.docType))
	if err != nil {
		logger.Error(err.Error())
	}
	return
}

func NewFileCommandStorage(name string) (storage *FileCommandStorage, err error) {
	file, err := os.Create(name)
	if err != nil {
		return
	}
	storage = &FileCommandStorage{file: file}
	return
}

type FileCommandStorage struct {
	file *os.File
}

func (f *FileCommandStorage) BulkSave(commands []*model.Command) (err error) {
	for _, cmd := range commands {
		f.file.WriteString(fmt.Sprintf("命令: %s\n", cmd.Input))
		f.file.WriteString(fmt.Sprintf("结果: %s\n", cmd.Output))
		f.file.WriteString("---\n")
	}
	return
}

type ServerReplayStorage struct {
	StorageType string
}

func (s *ServerReplayStorage) Upload(gZipFilePath, target string) (err error) {
	sessionID := strings.Split(filepath.Base(gZipFilePath), ".")[0]
	return service.PushSessionReplay(sessionID, gZipFilePath)
}

type OSSReplayStorage struct {
	endpoint  string
	bucket    string
	accessKey string
	secretKey string
}

func (o *OSSReplayStorage) Upload(gZipFilePath, target string) (err error) {
	client, err := oss.New(o.endpoint, o.accessKey, o.secretKey)
	if err != nil {
		return
	}
	bucket, err := client.Bucket(o.bucket)
	if err != nil {
		logger.Error(err.Error())
		return
	}
	return bucket.PutObjectFromFile(target, gZipFilePath)
}

type S3ReplayStorage struct {
	bucket    string
	region    string
	accessKey string
	secretKey string
	endpoint  string
}

func (s *S3ReplayStorage) Upload(gZipFilePath, target string) (err error) {

	file, err := os.Open(gZipFilePath)
	if err != nil {
		logger.Debug("Failed to open file", err)
		return
	}
	defer file.Close()
	s3Config := &aws.Config{
		Credentials: credentials.NewStaticCredentials(s.accessKey, s.secretKey, ""),
		Endpoint:    aws.String(s.endpoint),
		Region:      aws.String(s.region),
	}

	sess := session.Must(session.NewSession(s3Config))
	uploader := s3manager.NewUploader(sess, func(u *s3manager.Uploader) {
		u.PartSize = 64 * 1024 * 1024 // 64MB per part
	})
	_, err = uploader.Upload(&s3manager.UploadInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(target),
		Body:   file,
	})
	if err != nil {
		logger.Error(err.Error())
	}

	return
}

type AzureReplayStorage struct {
	accountName    string
	accountKey     string
	containerName  string
	endpointSuffix string
}

func (a *AzureReplayStorage) Upload(gZipFilePath, target string) (err error) {
	file, err := os.Open(gZipFilePath)
	if err != nil {
		return
	}

	credential, err := azblob.NewSharedKeyCredential(a.accountName, a.accountKey)
	if err != nil {
		logger.Error("Invalid credentials with error: " + err.Error())
	}
	p := azblob.NewPipeline(credential, azblob.PipelineOptions{})
	URL, _ := url.Parse(
		fmt.Sprintf("https://%s.blob.%s/%s", a.accountName, a.endpointSuffix, a.containerName))
	containerURL := azblob.NewContainerURL(*URL, p)
	blobURL := containerURL.NewBlockBlobURL(target)

	_, err = azblob.UploadFileToBlockBlob(context.TODO(), file, blobURL, azblob.UploadToBlockBlobOptions{
		BlockSize:   4 * 1024 * 1024,
		Parallelism: 16})
	if err != nil {
		logger.Error(err.Error())
	}
	return
}
