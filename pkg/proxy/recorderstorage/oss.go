package recorderstorage

import (
	"github.com/aliyun/aliyun-oss-go-sdk/oss"

	"github.com/jumpserver/koko/pkg/logger"
)

type OSSReplayStorage struct {
	Endpoint  string
	Bucket    string
	AccessKey string
	SecretKey string
}

func (o OSSReplayStorage) Upload(gZipFilePath, target string) (err error) {
	client, err := oss.New(o.Endpoint, o.AccessKey, o.SecretKey)
	if err != nil {
		return
	}
	bucket, err := client.Bucket(o.Bucket)
	if err != nil {
		logger.Error(err.Error())
		return
	}
	return bucket.PutObjectFromFile(target, gZipFilePath)
}
