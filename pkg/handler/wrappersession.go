package handler

import (
	"io"
	"strings"
	"sync"

	"github.com/gliderlabs/ssh"

	"github.com/jumpserver/koko/pkg/logger"
)

type WrapperSession struct {
	Sess      ssh.Session
	inWriter  io.WriteCloser
	outReader io.ReadCloser
	mux       *sync.RWMutex
}

func (w *WrapperSession) initial() {
	w.initReadPip()
	go w.readLoop()
}

func (w *WrapperSession) readLoop() {
	defer logger.Debug("session loop break")
	buf := make([]byte, 1024*8)
	for {
		nr, err := w.Sess.Read(buf)

		if nr > 0 {
			w.mux.RLock()
			_, _ = w.inWriter.Write(buf[:nr])
			w.mux.RUnlock()
		}
		if err != nil {
			break
		}
	}
	_ = w.inWriter.Close()

}

func (w *WrapperSession) Read(p []byte) (int, error) {
	w.mux.RLock()
	defer w.mux.RUnlock()
	return w.outReader.Read(p)
}

func (w *WrapperSession) Close() error {
	var err error
	err = w.inWriter.Close()
	w.initReadPip()
	return err
}

func (w *WrapperSession) Write(p []byte) (int, error) {
	return w.Sess.Write(p)
}

func (w *WrapperSession) initReadPip() {
	w.mux.Lock()
	defer w.mux.Unlock()
	w.outReader, w.inWriter = io.Pipe()
}

func (w *WrapperSession) Protocol() string {
	return "ssh"
}

func (w *WrapperSession) User() string {
	return w.Sess.User()
}

func (w *WrapperSession) WinCh() (winch <-chan ssh.Window) {
	_, winch, ok := w.Sess.Pty()
	if ok {
		return
	}
	return nil
}

func (w *WrapperSession) LoginFrom() string {
	return "ST"
}

func (w *WrapperSession) RemoteAddr() string {
	return strings.Split(w.Sess.RemoteAddr().String(), ":")[0]
}

func (w *WrapperSession) Pty() ssh.Pty {
	pty, _, _ := w.Sess.Pty()
	return pty
}

func NewWrapperSession(sess ssh.Session) *WrapperSession {
	w := &WrapperSession{
		Sess: sess,
		mux:  new(sync.RWMutex),
	}
	w.initial()
	return w
}
