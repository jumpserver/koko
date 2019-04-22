package service

import (
	"cocogo/pkg/logger"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"strings"

	"cocogo/pkg/common"
)

var (
	AccessKeyNotFound     = errors.New("access key not found")
	AccessKeyFileNotFound = errors.New("access key file not found")
	AccessKeyInvalid      = errors.New("access key not valid")
)

type AccessKey struct {
	Id      string
	Secret  string
	Path    string
	Context string
}

func (ak AccessKey) Sign() string {
	date := common.HTTPGMTDate()
	signature := common.MakeSignature(ak.Secret, date)
	return fmt.Sprintf("Sign %s:%s", ak.Id, signature)
}

func (ak *AccessKey) LoadAccessKeyFromStr(key string) error {
	if key == "" {
		return AccessKeyNotFound
	}
	keySlice := strings.Split(strings.TrimSpace(key), ":")
	if len(ak.Context) != 2 {
		return AccessKeyInvalid
	}
	ak.Id = keySlice[0]
	ak.Secret = keySlice[1]
	return nil
}

func (ak *AccessKey) LoadAccessKeyFromFile(keyPath string) error {
	if keyPath == "" {
		return AccessKeyNotFound
	}
	_, err := os.Stat(keyPath)
	if err != nil {
		return AccessKeyFileNotFound
	}
	buf, err := ioutil.ReadFile(keyPath)
	if err != nil {
		msg := fmt.Sprintf("read access key failed: %s", err)
		return errors.New(msg)
	}
	return ak.LoadAccessKeyFromStr(string(buf))
}

func (ak *AccessKey) SaveToFile() error {
	return nil
}

func (ak *AccessKey) Register(times int) error {
	return nil
}

// LoadAccessKey 加载AccessKey用来与 Core Api 交互
func (ak *AccessKey) Load() (err error) {
	err = ak.LoadAccessKeyFromStr(ak.Context)
	if err == nil {
		return
	}
	err = ak.LoadAccessKeyFromFile(ak.Path)
	if err == nil {
		return
	}
	err = ak.Register(10)
	if err != nil {
		msg := "register access key failed"
		logger.Error(msg)
		return errors.New(msg)
	}
	err = ak.SaveToFile()
	if err != nil {
		msg := fmt.Sprintf("save to access key to file error: %s", err)
		logger.Error(msg)
		return errors.New(msg)
	}
	return nil
}
