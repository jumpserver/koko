package main

import (
	"cocogo/pkg/auth"
	"cocogo/pkg/config"
	"cocogo/pkg/sshd"
)

var (
	conf       *config.Config
	appService *auth.Service
)

func init() {
	configFile := "config.yml"
	conf = config.LoadFromYaml(configFile)
	appService = auth.NewAuthService(conf)
	appService.LoadAccessKey()
	appService.EnsureValidAuth()
	appService.LoadTerminalConfig()
	sshd.Initial(conf, appService)
}

func main() {
	sshd.StartServer()
}
