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
	count := 0
	for count < 100 {
		user := getTerminalProfile()
		if user.Id != "" {
			break
		}
		msg := `Connect server error or access key is invalid,
				remove %s run again`
		logger.Errorf(msg, config.Conf.AccessKeyFile)
		time.Sleep(3 * time.Second)
		count++
		if count >= 3 {
			os.Exit(1)
		}
	}

}
