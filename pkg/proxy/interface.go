package proxy

import (
	"github.com/jumpserver/koko/pkg/model"
)

type proxyEngine interface {
	GenerateRecordCommand(s *commonSwitch, input, output string, riskLevel int64) *model.Command

	NewParser(s *commonSwitch) ParseEngine

	MapData(s *commonSwitch) map[string]interface{}
}

type ParseEngine interface {
	ParseStream(userInChan, srvInChan <-chan []byte) (userOut, srvOut <-chan []byte)

	Close()

	NeedRecord() bool

	CommandRecordChan() chan [3]string // [3]string{command, out, flag}
}
