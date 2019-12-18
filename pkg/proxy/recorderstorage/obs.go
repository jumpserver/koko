package recorderstorage

import (
	"os"

	"github.com/jumpserver/koko/pkg/logger"
	"github.com/jumpserver/koko/pkg/proxy/recorderstorage/obs"
)

type OBSReplayStorage struct {
	Endpoint  string
	Bucket    string
	AccessKey string
	SecretKey string
}

func (o OBSReplayStorage) Upload(gZipFilePath, target string) (err error) {
	file, err := os.Open(gZipFilePath)
	if err != nil {
		logger.Debug("Failed to open file", err)
		return err
	}
	defer file.Close()

	obsClient, err := obs.New(o.AccessKey, o.SecretKey, o.Endpoint)
	if err != nil {
		logger.Debug(err.Error())
		return err
	}

	input := &obs.PutFileInput{}
	input.Bucket = o.Bucket
	input.Key = target
	input.SourceFile = gZipFilePath

	output, err := obsClient.PutFile(input)
	if err != nil {
		logger.Debugf("StatusCode:%d, RequestId:%s\n", output.StatusCode, output.RequestId)
		logger.Debugf("ETag:%s, StorageClass:%s\n", output.ETag, output.StorageClass)
		return err
	}

	return nil
}
