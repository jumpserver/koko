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
	keyPath := config.Conf.AccessKeyFile
	client.BaseHost = config.Conf.CoreHost
	authClient.BaseHost = config.Conf.CoreHost
	client.SetHeader("X-JMS-ORG", "ROOT")
	authClient.SetHeader("X-JMS-ORG", "ROOT")

	if !path.IsAbs(config.Conf.AccessKeyFile) {
		keyPath = filepath.Join(config.Conf.RootPath, keyPath)
	}
	ak := AccessKey{Value: config.Conf.AccessKey, Path: keyPath}
	_ = ak.Load()
	authClient.Auth = ak
	validateAccessAuth()
	MustLoadServerConfigOnce()
	go KeepSyncConfigWithServer()
}

func validateAccessAuth() {
	maxTry := 30
	count := 0
	for {
		user, err := GetProfile()
		if err == nil && user.Role == "App" {
			break
		}
		if err != nil {
			msg := "Connect server error or access key is invalid, remove %s run again"
			logger.Errorf(msg, config.Conf.AccessKeyFile)
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
	conf := config.Conf
	conf.mu.Lock()
	defer conf.mu.Unlock()
	err = authClient.Get(TerminalConfigURL, conf)
	return err
}

func KeepSyncConfigWithServer() {
	for {
		logger.Debug("Sync config with server")
		err := LoadConfigFromServer()
		if err != nil {
			logger.Warn("Sync config with server error: ", err)
		}
		time.Sleep(60 * time.Second)
	}
}
