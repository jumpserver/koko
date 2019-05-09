package service

import (
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"strings"

	"cocogo/pkg/common"
	"cocogo/pkg/config"
	"cocogo/pkg/logger"
)

var (
	AccessKeyNotFound     = errors.New("access key not found")
	AccessKeyFileNotFound = errors.New("access key file not found")
	AccessKeyInvalid      = errors.New("access key not valid")
)

type AccessKey struct {
	Id     string
	Secret string
	Path   string
	Value  string
}

func (ak AccessKey) Sign() (string, string) {
	date := common.HTTPGMTDate()
	signature := common.MakeSignature(ak.Secret, date)
	return date, fmt.Sprintf("Sign %s:%s", ak.Id, signature)
}

func (ak *AccessKey) LoadAccessKeyFromStr(key string) error {
	if key == "" {
		return AccessKeyNotFound
	}
	keySlice := strings.Split(strings.TrimSpace(key), ":")
	if len(keySlice) != 2 {
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
	keyDir := path.Dir(ak.Path)
	if !common.FileExists(keyDir) {
		err := os.MkdirAll(keyDir, os.ModePerm)
		if err != nil {
			return err
		}
	}
	f, err := os.Create(ak.Path)
	defer f.Close()
	if err != nil {
		return err
	}
	_, err = f.WriteString(fmt.Sprintf("%s:%s", ak.Id, ak.Secret))
	if err != nil {
		logger.Error(err)
	}
	return err
}

func (ak *AccessKey) Register(times int) error {
	name := config.Conf.Name
	token := config.Conf.BootstrapToken
	comment := "Coco"

	res := RegisterTerminal(name, token, comment)
	if res.Name != name {
		msg := "register access key failed"
		logger.Error(msg)
		os.Exit(1)
	}
	ak.Id = res.ServiceAccount.AccessKey.Id
	ak.Secret = res.ServiceAccount.AccessKey.Secret
	return nil
}

// LoadAccessKey 加载AccessKey用来与 Core Api 交互
func (ak *AccessKey) Load() (err error) {
	err = ak.LoadAccessKeyFromStr(ak.Value)
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
