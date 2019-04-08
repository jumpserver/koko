package core

import (
	"github.com/sirupsen/logrus"
)

var log *logrus.Logger

const maxBufferSize = 1024 * 4

func init() {
	log = logrus.New()
}
