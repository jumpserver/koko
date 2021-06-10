package recorderstorage

import (
	"github.com/huaweicloud/huaweicloud-sdk-go-obs/obs"

	"github.com/jumpserver/koko/pkg/logger"
)

type OBSReplayStorage struct {
	Endpoint  string
	Bucket    string
	AccessKey string
	SecretKey string
}

func (o OBSReplayStorage) Upload(gZipFilePath, target string) (err error) {
	client, err := obs.New(o.AccessKey, o.SecretKey, o.Endpoint)
	if err != nil {
		return
	}
	input := &obs.PutFileInput{}
	input.Bucket = o.Bucket
	input.Key = target
	input.SourceFile = gZipFilePath
	_, err = client.PutFile(input)
	if err != nil {
		logger.Errorf("OBS upload file %s failed: %s", gZipFilePath, err)
		return err
	}
	return
}

func (o OBSReplayStorage) TypeName() string {
	return "obs"
}
