package proxybak

import (
	"context"

	uuid "github.com/satori/go.uuid"
)

type Transport interface {
	ReceiveChannel(ctx context.Context) chan<- []byte
	SendChannel(ctx context.Context) <-chan []byte
}

type Agent interface {
	ReceiveRequestChannel(ctx context.Context) chan<- []byte

	SendResponseChannel(ctx context.Context) <-chan []byte
}

func NewMemoryAgent(nConn Conn) *memoryAgent {

	m := &memoryAgent{
		conn:    nConn,
		inChan:  make(chan []byte),
		outChan: make(chan []byte),
	}
	return m
}

type memoryAgent struct {
	uuid    uuid.UUID
	conn    Conn
	inChan  chan []byte
	outChan chan []byte
}

func (m *memoryAgent) SendResponseChannel(ctx context.Context) <-chan []byte {
	go m.conn.SendResponse(ctx, m.outChan)
	return m.outChan
}

func (m *memoryAgent) ReceiveRequestChannel(ctx context.Context) chan<- []byte {
	go m.conn.ReceiveRequest(ctx, m.inChan, m.outChan)
	return m.inChan
}
