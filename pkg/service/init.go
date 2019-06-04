package service

import (
	"encoding/json"
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
	cf := config.GetConf()
	keyPath := cf.AccessKeyFile
	client.BaseHost = cf.CoreHost
	authClient.BaseHost = cf.CoreHost
	client.SetHeader("X-JMS-ORG", "ROOT")
	authClient.SetHeader("X-JMS-ORG", "ROOT")

	if !path.IsAbs(cf.AccessKeyFile) {
		keyPath = filepath.Join(cf.RootPath, keyPath)
	}
	ak := AccessKey{Value: cf.AccessKey, Path: keyPath}
	_ = ak.Load()
	authClient.Auth = ak
	validateAccessAuth()
	MustLoadServerConfigOnce()
	go KeepSyncConfigWithServer()
}

func validateAccessAuth() {
	cf := config.GetConf()
	maxTry := 30
	count := 0
	for {
		user, err := GetProfile()
		if err == nil && user.Role == "App" {
			break
		}
		if err != nil {
			msg := "Connect server error or access key is invalid, remove %s run again"
			logger.Errorf(msg, cf.AccessKeyFile)
		} else if user.Role != "App" {
			logger.Error("Access role is not App, is: ", user.Role)
		}
		count++
		time.Sleep(3 * time.Second)
		if count >= maxTry {
			os.Exit(1)
		}
	}
}

func MustLoadServerConfigOnce() {
	var data map[string]interface{}
	err := authClient.Get(TerminalConfigURL, &data)
	if err != nil {
		logger.Error("Load config from server error: ", err)
		return
	}
	data["TERMINAL_HOST_KEY"] = "Hidden"
	msg, err := json.Marshal(data)
	if err != nil {
		logger.Error("Marsha server config error: %s", err)
		return
	}
	logger.Debug("Load config from server: " + string(msg))
	err = LoadConfigFromServer()
	if err != nil {
		logger.Error("Load config from server error: ", err)
	}
}

func LoadConfigFromServer() (err error) {
	conf := config.GetConf()
	err = authClient.Get(TerminalConfigURL, conf)
	if err != nil {
		return err
	}
	config.SetConf(conf)
	return nil
}

func KeepSyncConfigWithServer() {
	for {
		err := LoadConfigFromServer()
		if err != nil {
			logger.Warn("Sync config with server error: ", err)
		}
		time.Sleep(60 * time.Second)
	}
}
