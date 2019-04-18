package auth

import (
	"cocogo/pkg/config"
)

var appService *Service

func Initial() {
	conf := config.GetGlobalConfig()
	appService = NewAuthService(conf)
	appService.LoadAccessKey()
	appService.EnsureValidAuth()
	appService.LoadTerminalConfig()
}

func GetGlobalService() *Service {
	if appService == nil {
		Initial()
	}
	return appService
}
