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
	// 创建ObsClient结构体
	obsClient, err := obs.New(o.AccessKey, o.SecretKey, o.Endpoint)
	if err != nil {
		return err
	}
	defer obsClient.Close()

	input := &obs.PutFileInput{}
	input.Bucket = o.Bucket
	input.Key = target
	// SourceFile为待上传的本地文件路径，需要指定到具体的文件名
	input.SourceFile = gZipFilePath
	output, err := obsClient.PutFile(input)
	if err != nil {
		logger.Error(err.Error())
		if obsError, ok := err.(obs.ObsError); ok {
			logger.Errorf("Code:%s\n", obsError.Code)
			logger.Errorf("Output:%s\n", output)
			logger.Errorf("Message:%s\n", obsError.Message)
		}
	}
	return err
}

func (o OBSReplayStorage) TypeName() string {
	return "obs"
}
