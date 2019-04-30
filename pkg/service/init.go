package service

import (
	"path"
	"path/filepath"
	"strings"

	"cocogo/pkg/common"
	"cocogo/pkg/config"
)

var client = common.NewClient(10)
var authClient = common.NewClient(10)
var baseHost string

func Initial() {
	keyPath := config.Conf.AccessKeyFile
	baseHost = strings.TrimRight(config.Conf.CoreHost, "/")

	if !path.IsAbs(config.Conf.AccessKeyFile) {
		keyPath = filepath.Join(config.Conf.RootPath, keyPath)
	}
	ak := AccessKey{Value: config.Conf.AccessKey, Path: keyPath}
	_ = ak.Load()
	authClient.Auth = ak
}
