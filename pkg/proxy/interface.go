package proxy

import (
	"github.com/jumpserver/koko/pkg/exchange"
)

type ParseEngine interface {
	ParseStream(userInChan chan *exchange.RoomMessage, srvInChan <-chan []byte) (userOut, srvOut <-chan []byte)

	Close()

	NeedRecord() bool

	CommandRecordChan() chan *ExecutedCommand
}
