package proxy

import "context"

type Conn interface {
	ReceiveRequest(context.Context, <-chan []byte, chan<- []byte)
	SendResponse(context.Context, chan<- []byte)
}
