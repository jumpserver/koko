package coco

import (
	"cocogo/pkg/config"
	"cocogo/pkg/logger"
	"cocogo/pkg/service"
)

func init() {
	config.Initial()
	logger.Initial()
	service.Initial()
}
