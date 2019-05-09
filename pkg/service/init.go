package service

import (
	"os"
	"path"
	"path/filepath"
	"time"

	"cocogo/pkg/common"
	"cocogo/pkg/config"
	"cocogo/pkg/logger"
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
	ValidateAccessAuth()
}

func ValidateAccessAuth() {
	maxTry := 30
	count := 0
	for count < maxTry {
		user, err := GetProfile()
		if err == nil && user.Role == "App" {
			break
		}
		if err != nil {
			msg := `Connect server error or access key is invalid, remove %s run again`
			logger.Errorf(msg, config.Conf.AccessKeyFile)
		}
		if user.Role != "App" {
			logger.Error("Access role is not App, is: ", user.Role)
		}
		time.Sleep(3 * time.Second)
		count++
		if count >= maxTry {
			os.Exit(1)
		}
	}

}
