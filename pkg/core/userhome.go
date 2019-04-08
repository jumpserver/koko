package core

import (
	"context"
	"io"
	"sync"

	"github.com/gliderlabs/ssh"
	uuid "github.com/satori/go.uuid"
)

type Conn interface {
	SessionID() string

	User() string

	UUID() uuid.UUID

	Pty() (ssh.Pty, <-chan ssh.Window, bool)

	Context() context.Context

	io.Reader
	io.WriteCloser
}

type SessionHome interface {
	SessionID() string
	AddConnection(c Conn)
	RemoveConnection(c Conn)
	SendRequestChannel(ctx context.Context) <-chan []byte
	ReceiveResponseChannel(ctx context.Context) chan<- []byte
}

func NewUserSessionHome(con Conn) *userSessionHome {
	return &userSessionHome{
		readStream: make(chan []byte),
		mainConn:   con,
		connMap:    map[string]Conn{con.UUID().String(): con},
		cancelMap:  map[string]context.CancelFunc{},
	}

}

type userSessionHome struct {
	readStream chan []byte
	mainConn   Conn
	connMap    map[string]Conn
	cancelMap  map[string]context.CancelFunc
	sync.RWMutex
}

func (r *userSessionHome) SessionID() string {
	return r.mainConn.SessionID()
}

func (r *userSessionHome) AddConnection(c Conn) {

	key := c.SessionID()
	if _, ok := r.connMap[key]; !ok {
		log.Info("add connection ", c)
		r.connMap[key] = c
	} else {
		log.Info("already add connection")
		return
	}

	log.Info("add conn session room: ", r.SessionID())

	ctx, cancelFunc := context.WithCancel(r.mainConn.Context())
	r.cancelMap[key] = cancelFunc

	defer r.RemoveConnection(c)

	buf := make([]byte, maxBufferSize)
	for {
		nr, err := c.Read(buf)
		if err != nil {
			log.Error("conn read err")
			return
		}

		select {
		case <-ctx.Done():
			log.Info("conn ctx done")
			return
		default:
			r.readStream <- buf[:nr]

		}

	}

}

func (r *userSessionHome) RemoveConnection(c Conn) {
	r.Lock()
	defer r.Unlock()
	key := c.SessionID()
	if _, ok := r.connMap[key]; ok {
		delete(r.connMap, key)
		delete(r.cancelMap, key)
	}
}

func (r *userSessionHome) SendRequestChannel(ctx context.Context) <-chan []byte {
	go func() {
		buf := make([]byte, 1024)
		// 从发起的session这里关闭 接受的通道
		defer close(r.readStream)
		for {
			nr, e := r.mainConn.Read(buf)
			if e != nil {
				log.Error("main Conn read err")
				break
			}
			select {
			case <-ctx.Done():
				return
			default:
				r.readStream <- buf[:nr]
			}

		}

	}()
	return r.readStream
}

func (r *userSessionHome) ReceiveResponseChannel(ctx context.Context) chan<- []byte {

	writeStream := make(chan []byte)
	go func() {
		defer func() {
			r.RLock()
			for _, cancel := range r.cancelMap {
				cancel()
			}
			r.RUnlock()
		}()

		for {
			select {
			case <-ctx.Done():
				return
			case buf, ok := <-writeStream:
				if !ok {
					return
				}
				for _, c := range r.connMap {
					nw, err := c.Write(buf)
					if err != nil || nw != len(buf) {
						log.Error("Write Conn err", c)
						r.cancelMap[c.SessionID()]()
					}
				}

			}
		}

	}()
	return writeStream
}
