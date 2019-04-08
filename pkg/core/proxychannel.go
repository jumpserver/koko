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

func NewMemoryChannel(n *NodeConn) *memoryChannel {
	m := &memoryChannel{
		uuid: n.UUID(),
		conn: n,
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
	// 传入context， 可以从外层进行取消
	sendChannel := make(chan []byte)

	go func() {
		defer close(sendChannel)

		resp := make([]byte, maxBufferSize)
		for {
			nr, e := m.conn.Read(resp)
			if e != nil {
				log.Info("m.conn.Read(resp) err: ", e)
				break
			}

			select {
			case <-ctx.Done():
				return
			default:
				sendChannel <- resp[:nr]
			}

		}

	}()

	return sendChannel
}

func (m *memoryChannel) ReceiveRequestChannel(ctx context.Context) chan<- []byte {
	// 传入context， 可以从外层进行取消
	receiveChannel := make(chan []byte)
	go func() {
		defer m.conn.Close()
		for {
			select {
			case <-ctx.Done():
				log.Info("ReceiveRequestChannel ctx done")
				return
			case reqBuf, ok := <-receiveChannel:
				if !ok {
					return
				}
				nw, e := m.conn.Write(reqBuf)
				if e != nil && nw != len(reqBuf) {
					return
				}
			}
		}

	}()

	return receiveChannel
}

func (m *memoryChannel) Wait() error {
	return m.conn.Wait()
}
