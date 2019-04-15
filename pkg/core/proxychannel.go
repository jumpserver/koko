package core

import (
	"context"

	uuid "github.com/satori/go.uuid"
)

type ProxyChannel interface {
	UUID() string
	ReceiveRequestChannel(ctx context.Context) chan<- []byte

	SendResponseChannel(ctx context.Context) <-chan []byte

	Wait() error
}

func NewMemoryChannel(nConn *NodeConn, useS Conn) *memoryChannel {

	m := &memoryChannel{
		uuid: nConn.UUID(),
		conn: nConn,
	}
	return m
}

type memoryChannel struct {
	uuid uuid.UUID
	conn *NodeConn
}

func (m *memoryChannel) UUID() string {
	return m.uuid.String()
}

func (m *memoryChannel) SendResponseChannel(ctx context.Context) <-chan []byte {
	go m.conn.handleResponse(ctx)
	return m.conn.outChan
}

func (m *memoryChannel) ReceiveRequestChannel(ctx context.Context) chan<- []byte {
	go m.conn.handleRequest(ctx)
	return m.conn.inChan
}

func (m *memoryChannel) Wait() error {
	return m.conn.Wait()
}
