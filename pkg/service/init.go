package service

import (
	"cocogo/pkg/common"
	"cocogo/pkg/config"
	"path"
	"path/filepath"
)

var client = common.NewClient(30, "")
var authClient = common.NewClient(30, "")

func Initial() {
	keyPath := config.Conf.AccessKeyFile
	client.BaseHost = config.Conf.CoreHost
	authClient.BaseHost = config.Conf.CoreHost

	if !path.IsAbs(config.Conf.AccessKeyFile) {
		keyPath = filepath.Join(config.Conf.RootPath, keyPath)
	}
	ak := AccessKey{Value: config.Conf.AccessKey, Path: keyPath}
	_ = ak.Load()
	authClient.Auth = ak
}
