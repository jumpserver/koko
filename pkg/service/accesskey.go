package service

import (
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"strings"
	"time"

	"github.com/jumpserver/koko/pkg/common"
	"github.com/jumpserver/koko/pkg/config"
	"github.com/jumpserver/koko/pkg/logger"
)

var (
	AccessKeyNotFound     = errors.New("access key not found")
	AccessKeyFileNotFound = errors.New("access key file not found")
	AccessKeyInvalid      = errors.New("access key not valid")
	AccessKeyUnauthorized = errors.New("access key unauthorized")
)

type AccessKey struct {
	ID     string
	Secret string
	Path   string
	Value  string
}

func (ak AccessKey) Sign() (string, string) {
	date := common.HTTPGMTDate()
	signature := common.MakeSignature(ak.Secret, date)
	return date, fmt.Sprintf("Sign %s:%s", ak.ID, signature)
}

func (ak *AccessKey) LoadAccessKeyFromStr(key string) error {
	if key == "" {
		return AccessKeyNotFound
	}
	keySlice := strings.Split(strings.TrimSpace(key), ":")
	if len(keySlice) != 2 {
		return AccessKeyInvalid
	}
	ak.ID = keySlice[0]
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
	if _, err := os.Stat(ak.Path); err == nil {
		bakFilePath := fmt.Sprintf("%s_%s", ak.Path,
			time.Now().Format("2006-01-02_15-04-05"))
		if err2 := os.Rename(ak.Path, bakFilePath); err2 != nil {
			logger.Errorf("Rename %s to %s err: %s",
				ak.Path, bakFilePath, err)
		}
	}
	f, err := os.Create(ak.Path)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(fmt.Sprintf("%s:%s", ak.ID, ak.Secret))
	if err != nil {
		logger.Error(err)
	}
	return err
}

func (ak *AccessKey) Register(times int) error {
	cf := config.GetConf()
	name := cf.Name
	token := cf.BootstrapToken
	comment := "Coco"

	res := RegisterTerminal(name, token, comment)
	if res.Name != name {
		msg := "register access key failed"
		logger.Error(msg)
		os.Exit(1)
	}
	ak.ID = res.ServiceAccount.AccessKey.ID
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
	return ak.RegisterKey()
}

func (ak *AccessKey) RegisterKey() (err error) {
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
