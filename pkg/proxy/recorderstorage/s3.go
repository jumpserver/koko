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
		logger.Debug("Failed to open file", err)
		return
	}
	defer file.Close()
	s3Config := &aws.Config{
		Credentials: credentials.NewStaticCredentials(s.AccessKey, s.SecretKey, ""),
		Endpoint:    aws.String(s.Endpoint),
		Region:      aws.String(s.Region),
	}

	sess := session.Must(session.NewSession(s3Config))
	uploader := s3manager.NewUploader(sess, func(u *s3manager.Uploader) {
		u.PartSize = 64 * 1024 * 1024 // 64MB per part
	})
	_, err = uploader.Upload(&s3manager.UploadInput{
		Bucket: aws.String(s.Bucket),
		Key:    aws.String(target),
		Body:   file,
	})
	if err != nil {
		logger.Error(err.Error())
	}

	return
}
