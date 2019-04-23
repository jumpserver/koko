package sdk

import (
	"path"
	"path/filepath"

	"cocogo/pkg/common"
	"cocogo/pkg/config"
)

type ClientAuth interface {
	Sign() string
}

type WrapperClient struct {
	Http     *common.Client
	Auth     ClientAuth
	BaseHost string
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
	var user User
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
