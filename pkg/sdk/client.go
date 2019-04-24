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
	Http       *common.Client
	AuthClient *common.Client
	Auth       ClientAuth
	BaseHost   string
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
<<<<<<< HEAD
	//if needAuth {
	//	c.Http.SetAuth(c.Auth.Sign())
	//} else {
	//	c.Http.SetAuth("")
	//}
=======
	if needAuth {
		return c.AuthClient.Get(c.BaseHost+url, res)
	} else {
		return c.Http.Get(c.BaseHost+url, res)
	}
>>>>>>> 228056688660034ec5b8061e99c187aa0b1c85e7

}

func (c *WrapperClient) Post(url string, data interface{}, res interface{}, needAuth bool) error {
<<<<<<< HEAD
	//if needAuth {
	//	c.Http.SetAuth(c.Auth.Sign())
	//} else {
	//	c.Http.SetAuth("")
	//}
	return c.Http.Post(url, data, res)
}

func (c *WrapperClient) Delete(url string, res interface{}, needAuth bool) error {
	//if needAuth {
	//	c.Http.SetAuth(c.Auth.Sign())
	//} else {
	//	c.Http.SetAuth("")
	//}
	return c.Http.Delete(url, res)
}

func (c *WrapperClient) Put(url string, data interface{}, res interface{}, needAuth bool) error {
	//if needAuth {
	//	c.Http.SetAuth(c.Auth.Sign())
	//} else {
	//	c.Http.SetAuth("")
	//}
	return c.Http.Put(url, data, res)
}

func (c *WrapperClient) Patch(url string, data interface{}, res interface{}, needAuth bool) error {
	//if needAuth {
	//	c.Http.SetAuth(c.Auth.Sign())
	//} else {
	//	c.Http.SetAuth("")
	//}
	return c.Http.Patch(url, data, res)
=======
	if needAuth {
		return c.AuthClient.Post(url, data, res)
	} else {
		return c.Http.Post(url, data, res)
	}
}

func (c *WrapperClient) Delete(url string, res interface{}, needAuth bool) error {
	if needAuth {
		return c.AuthClient.Delete(url, res)
	} else {
		return c.Http.Delete(url, res)
	}
}

func (c *WrapperClient) Put(url string, data interface{}, res interface{}, needAuth bool) error {
	if needAuth {
		return c.AuthClient.Put(url, data, res)
	} else {
		return c.Http.Put(url, data, res)
	}
}

func (c *WrapperClient) Patch(url string, data interface{}, res interface{}, needAuth bool) error {
	if needAuth {
		return c.AuthClient.Patch(url, data, res)
	} else {
		return c.Http.Patch(url, data, res)
	}
>>>>>>> 228056688660034ec5b8061e99c187aa0b1c85e7
}
