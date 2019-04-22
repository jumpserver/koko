package service

import (
	"os"

	"cocogo/pkg/logger"
)

var client = WrapperClient{}

func init() {
	err := client.LoadAuth()
	if err != nil {
		logger.Error("Load client access key error: %s", err)
		os.Exit(10)
	}
	err = client.CheckAuth()
	if err != nil {
		logger.Error("Check client auth error: %s", err)
		os.Exit(11)
	}
}
