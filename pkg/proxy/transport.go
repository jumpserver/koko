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
	readWriter io.ReadWriteCloser
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
	if dt.closed {
		return nil
	}
	logger.Debug("Close transport")
	dt.closed = true
	return dt.readWriter.Close()
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
		dt.ch <- buf[:n]
	}
	close(dt.ch)
}

func NewDirectTransport(name string, readWriter io.ReadWriteCloser) Transport {
	ch := make(chan []byte, 1024)
	tr := DirectTransport{readWriter: readWriter, ch: ch}
	go tr.Keep()
	return &tr
}
