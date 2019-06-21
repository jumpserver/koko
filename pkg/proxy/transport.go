package proxy

import (
	"io"

	"github.com/jumpserver/koko/pkg/logger"
)

type Transport interface {
	io.WriteCloser
	Name() string
	Chan() <-chan []byte
}

type DirectTransport struct {
	name       string
	readWriter io.ReadWriter
	ch         chan []byte
	closed     bool
}

func (dt *DirectTransport) Name() string {
	return dt.name
}

func (dt *DirectTransport) Write(p []byte) (n int, err error) {
	return dt.readWriter.Write(p)
}

func (dt *DirectTransport) Close() error {
	logger.Debug("Close transport")
	if dt.closed {
		return nil
	}
	dt.closed = true
	close(dt.ch)
	return nil
}

func (dt *DirectTransport) Chan() <-chan []byte {
	return dt.ch
}

func (dt *DirectTransport) Keep() {
	for {
		buf := make([]byte, 1024)
		n, err := dt.readWriter.Read(buf)
		if err != nil {
			_ = dt.Close()
			break
		}
		if !dt.closed {
			dt.ch <- buf[:n]
		} else {
			// 解决用户终端返回时, 数据丢失
			_ , _ = dt.readWriter.Write(buf[:n])
			logger.Debug("Transport closed")
			break
		}
	}
	return
}

func NewDirectTransport(name string, readWriter io.ReadWriter) Transport {
	ch := make(chan []byte, 1024*32)
	tr := DirectTransport{readWriter: readWriter, ch: ch}
	go tr.Keep()
	return &tr
}
