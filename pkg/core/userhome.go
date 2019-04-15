package core

import (
	"context"
	"sync"
)

type SessionHome interface {
	SessionID() string
	AddConnection(c Conn)
	RemoveConnection(c Conn)
	SendRequestChannel(ctx context.Context) <-chan []byte
	ReceiveResponseChannel(ctx context.Context) chan<- []byte
}

func NewUserSessionHome(con Conn) *userSessionHome {
	uHome := &userSessionHome{
		readStream: make(chan []byte),
		mainConn:   con,
		connMap:    new(sync.Map),
		cancelMap:  new(sync.Map),
	}
	uHome.connMap.Store(con.SessionID(), con)
	return uHome

}

type userSessionHome struct {
	readStream chan []byte
	mainConn   Conn
	connMap    *sync.Map
	cancelMap  *sync.Map
}

func (r *userSessionHome) SessionID() string {
	return r.mainConn.SessionID()
}

func (r *userSessionHome) AddConnection(c Conn) {

	key := c.SessionID()
	if _, ok := r.connMap.Load(key); !ok {
		log.Info("add connection ", c)
		r.connMap.Store(key, c)
	} else {
		log.Info("already add connection")
		return
	}

	log.Info("add conn session room: ", r.SessionID())

	ctx, cancelFunc := context.WithCancel(r.mainConn.Context())
	r.cancelMap.Store(key, cancelFunc)

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
			log.Info(" user conn ctx done")
			return
		default:
			copyBuf := make([]byte, nr)
			copy(copyBuf, buf[:nr])
			r.readStream <- copyBuf

		}

	}

}

func (r *userSessionHome) RemoveConnection(c Conn) {

	key := c.SessionID()
	if cancelFunc, ok := r.cancelMap.Load(key); ok {
		cancelFunc.(context.CancelFunc)()
	}
	r.connMap.Delete(key)

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
				var respCopy []byte
				respCopy = append(respCopy, buf[:nr]...)
				r.readStream <- respCopy
			}

		}

	}()
	return r.readStream
}

func (r *userSessionHome) ReceiveResponseChannel(ctx context.Context) chan<- []byte {

	writeStream := make(chan []byte)
	go func() {
		defer func() {
			r.cancelMap.Range(func(key, cancelFunc interface{}) bool {
				cancelFunc.(context.CancelFunc)()
				return true
			})
		}()

		for {
			select {
			case <-ctx.Done():
				return
			case buf, ok := <-writeStream:
				if !ok {
					return
				}
				r.connMap.Range(func(key, connItem interface{}) bool {
					nw, err := connItem.(Conn).Write(buf)
					if err != nil || nw != len(buf) {
						log.Error("Write Conn err", connItem)
						r.RemoveConnection(connItem.(Conn))

					}
					return true
				})

			}
		}

	}()
	return writeStream
}
