package service

import (
	"cocogo/pkg/model"
	"net/http"
	"path"
	"path/filepath"
	"strings"

	"cocogo/pkg/common"
	"cocogo/pkg/config"
)

type ClientAuth interface {
	Sign() string
}

type WrapperClient struct {
	*common.Client
	Auth     ClientAuth
	BaseHost string
}

func (c *WrapperClient) SetAuthHeader(r *http.Request) {
	if c.Auth != nil {
		signature := c.Auth.Sign()
		r.Header.Add("Authorization", signature)
	}
}

func (c *WrapperClient) ExpandUrl(url string, query map[string]string) string {
	return ""
}

func (c *WrapperClient) ParseUrl(url string, params ...map[string]string) string {
	var newUrl = ""
	if url, ok := urls[url]; ok {
		newUrl = url
	}
	if c.BaseHost != "" {
		newUrl = strings.TrimRight(c.BaseHost, "/") + newUrl
	}
	if len(params) == 1 {
		url = c.ParseUrlQuery(url, params[0])
	}
	return newUrl
}

func (c *WrapperClient) LoadAuth() error {
	keyPath := config.Conf.AccessKeyFile
	if !path.IsAbs(config.Conf.AccessKeyFile) {
		keyPath = filepath.Join(config.Conf.RootPath, keyPath)
	}
	ak := AccessKey{Value: config.Conf.AccessKey, Path: keyPath}
	err := ak.Load()
	if err != nil {
		return err
	}
	c.Auth = ak
	return nil
}

func (c *WrapperClient) CheckAuth() error {
	var user model.User
	err := c.Get("UserProfileUrl", &user)
	if err != nil {
		return err
	}
	return nil
}
