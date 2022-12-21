package model

import (
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"strings"
	"time"
)

var (
	AccessKeyNotFound     = errors.New("access key not found")
	AccessKeyFileNotFound = errors.New("access key file not found")
	AccessKeyInvalid      = errors.New("access key not valid")
)

type AccessKey struct {
	ID     string `json:"id"`
	Secret string `json:"secret"`
}

func (ak *AccessKey) LoadFromStr(key string) error {
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

func (ak *AccessKey) LoadFromFile(keyPath string) error {
	if keyPath == "" {
		return AccessKeyNotFound
	}
	if _, err := os.Stat(keyPath); err != nil {
		return AccessKeyFileNotFound
	}
	buf, err := ioutil.ReadFile(keyPath)
	if err != nil {
		msg := fmt.Sprintf("read file failed: %s", err)
		return fmt.Errorf("%w: %s", AccessKeyInvalid, msg)
	}
	return ak.LoadFromStr(string(buf))
}

func (ak *AccessKey) SaveToFile(path string) error {
	if _, err := os.Stat(path); err == nil {
		bakPath := fmt.Sprintf("%s_%s", path,
			time.Now().Format("2006-01-02_15-04-05"))
		if err2 := os.Rename(path, bakPath); err2 != nil {
			return err2
		}
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(fmt.Sprintf("%s:%s", ak.ID, ak.Secret))
	return err
}
