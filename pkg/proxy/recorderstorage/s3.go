package recorderstorage

import (
	"os"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"

	"github.com/jumpserver/koko/pkg/logger"
)

type S3ReplayStorage struct {
	Bucket    string
	Region    string
	AccessKey string
	SecretKey string
	Endpoint  string
}

func (s S3ReplayStorage) Upload(gZipFilePath, target string) (err error) {

	file, err := os.Open(gZipFilePath)
	if err != nil {
		logger.Errorf("Open %s file failed: %s", gZipFilePath, err)
		return err
	}
	defer file.Close()
	s3Config := &aws.Config{
		Endpoint:         aws.String(s.Endpoint),
		Region:           aws.String(s.Region),
		S3ForcePathStyle: aws.Bool(true),
	}
	if s.AccessKey != "" && s.SecretKey != "" {
		s3Config.Credentials = credentials.NewStaticCredentials(s.AccessKey, s.SecretKey, "")
	}
	sess, err := session.NewSession(s3Config)
	if err != nil {
		logger.Errorf("S3 new session failed: %s", err)
		return err
	}

	uploader := s3manager.NewUploader(sess, func(u *s3manager.Uploader) {
		u.PartSize = 64 * 1024 * 1024 // 64MB per part
	})
	_, err = uploader.Upload(&s3manager.UploadInput{
		Bucket: aws.String(s.Bucket),
		Key:    aws.String(target),
		Body:   file,
	})
	if err != nil {
		logger.Errorf("S3 upload file %s failed: %s", gZipFilePath, err)
		return err
	}

	return
}

func (s S3ReplayStorage) TypeName() string {
	return "s3"
}
