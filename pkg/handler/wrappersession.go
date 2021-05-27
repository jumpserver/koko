package handler

import (
	"context"
	"io"
	"net"
	"sync"

	"github.com/gliderlabs/ssh"

	"github.com/jumpserver/koko/pkg/common"
	"github.com/jumpserver/koko/pkg/logger"
)

type WrapperSession struct {
	Uuid      string
	Sess      ssh.Session
	inWriter  io.WriteCloser
	outReader io.ReadCloser
	mux       *sync.RWMutex

	closed chan struct{}

	winch      chan ssh.Window
	currentWin ssh.Window
}

func (w *WrapperSession) initial() {
	w.Uuid = common.UUID()
	w.closed = make(chan struct{})
	w.initReadPip()
	go w.readLoop()
}

func (w *WrapperSession) readLoop() {
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
	w.mux.RLock()
	_ = w.inWriter.Close()
	_ = w.outReader.Close()
	w.mux.RUnlock()
	close(w.closed)
	logger.Infof("Request %s: Read loop break", w.Uuid)
}

func (w *WrapperSession) Read(p []byte) (int, error) {
	select {
	case <-w.closed:
		return 0, io.EOF
	default:
	}
	w.mux.RLock()
	defer w.mux.RUnlock()
	return w.outReader.Read(p)
}

func (w *WrapperSession) Close() error {
	select {
	case <-w.closed:
		return nil
	default:
	}
	_ = w.inWriter.Close()
	err := w.outReader.Close()
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

func (w *WrapperSession) Context() context.Context {
	return w.Sess.Context()
}

func (w *WrapperSession) WinCh() (winch <-chan ssh.Window) {
	return w.winch
}

func (w *WrapperSession) SetWin(win ssh.Window) {
	select {
	case w.winch <- win:
	default:
	}
	w.currentWin = win
}

func (w *WrapperSession) LoginFrom() string {
	return "ST"
}

func (w *WrapperSession) RemoteAddr() string {
	host, _, _ := net.SplitHostPort(w.Sess.RemoteAddr().String())
	return host
}

func (w *WrapperSession) Pty() ssh.Pty {
	pty, _, _ := w.Sess.Pty()
	termWin := w.currentWin
	if w.currentWin.Width == 0 || w.currentWin.Height == 0 {
		termWin = pty.Window
	}
	return ssh.Pty{
		Term:   pty.Term,
		Window: termWin,
	}
}

func (w *WrapperSession) ID() string {
	return w.Uuid
}

func NewWrapperSession(sess ssh.Session) *WrapperSession {
	w := &WrapperSession{
		Sess:  sess,
		mux:   new(sync.RWMutex),
		winch: make(chan ssh.Window),
	}
	w.initial()
	return w
}
