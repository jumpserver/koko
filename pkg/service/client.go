package service

import (
	"cocogo/pkg/common"
	"cocogo/pkg/config"
	"cocogo/pkg/model"
	"path"
	"path/filepath"
	"strings"
)

type ClientAuth interface {
	Sign() string
}

type WrapperClient struct {
	Http     *common.Client
	Auth     ClientAuth
	BaseHost string
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
		url = c.Http.ParseUrlQuery(url, params[0])
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
	err := c.Http.Get("UserProfileUrl", &user)
	if err != nil {
		return err
	}
	return nil
}

func (c *WrapperClient) Get(url string, res interface{}, needAuth bool) error {
	if needAuth {
		c.Http.SetAuth(c.Auth.Sign())
	}

	return c.Http.Get(c.BaseHost+url, res)
}
