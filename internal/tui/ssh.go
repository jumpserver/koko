// Package tui provides the SSH screen and embedded terminal used by Koko's TUI.
package tui

import (
	"io"
	"sync"

	"github.com/gdamore/tcell/v2"
	"github.com/gdamore/tcell/v2/terminfo"
	_ "github.com/gdamore/tcell/v2/terminfo/extended"
	"github.com/gliderlabs/ssh"
)

// sshTTY owns the only reader of the login channel. Draining the screen releases
// tcell's reader without closing the SSH channel before it restores terminal modes.
type sshTTY struct {
	session ssh.Session
	reader  *io.PipeReader
	writer  *io.PipeWriter
	mu      sync.Mutex
	window  ssh.Window
	resize  func()
	start   sync.Once
	stop    sync.Once
	done    chan struct{}
}

func NewSSHScreen(session ssh.Session, windows <-chan ssh.Window) (tcell.Screen, error) {
	pty, _, _ := session.Pty()
	ti, err := terminfo.LookupTerminfo(pty.Term)
	if err != nil {
		return nil, err
	}
	if ti.SetCursor == "" {
		return nil, terminfo.ErrTermNotFound
	}
	// SSH provides flow control; serial-terminal padding would sleep on every
	// cursor/style change (notably VT100). Do not mutate shared terminfo entries.
	networkInfo := *ti
	networkInfo.PadChar = ""
	r, w := io.Pipe()
	tty := &sshTTY{session: session, reader: r, writer: w, window: pty.Window, done: make(chan struct{})}
	screen, err := tcell.NewTerminfoScreenFromTtyTerminfo(tty, &networkInfo)
	if err != nil {
		_ = tty.Close()
		return nil, err
	}
	go func() {
		for {
			select {
			case win, ok := <-windows:
				if !ok {
					return
				}
				tty.mu.Lock()
				tty.window = win
				cb := tty.resize
				tty.mu.Unlock()
				if cb != nil {
					cb()
				}
			case <-tty.done:
				return
			case <-session.Context().Done():
				return
			}
		}
	}()
	cursorReset := ti.CursorDefault
	if cursorReset == "" && (ti.Mouse != "" || ti.XTermLike) {
		cursorReset = "\x1b[0 q"
	}
	return &sshScreen{Screen: screen, output: session, cursorReset: cursorReset}, nil
}

// SetScreen calls Init itself and ignores errors; initialize once explicitly.
type sshScreen struct {
	tcell.Screen
	once        sync.Once
	err         error
	fini        sync.Once
	output      io.Writer
	cursorReset string
}

func (s *sshScreen) Init() error { s.once.Do(func() { s.err = s.Screen.Init() }); return s.err }

func (s *sshScreen) Fini() {
	s.fini.Do(func() {
		s.Screen.Fini()
		// tcell may not emit a pending default style after hiding the cursor.
		// Restore it while the SSH channel is still writable.
		if s.err == nil && s.cursorReset != "" {
			_, _ = io.WriteString(s.output, s.cursorReset)
		}
	})
}

func (t *sshTTY) Start() error {
	t.start.Do(func() {
		go func() {
			buf := make([]byte, 8192)
			_, err := io.CopyBuffer(t.writer, t.session, buf)
			_ = t.writer.CloseWithError(err)
		}()
	})
	return nil
}
func (t *sshTTY) Read(b []byte) (int, error)  { return t.reader.Read(b) }
func (t *sshTTY) Write(b []byte) (int, error) { return t.session.Write(b) }
func (t *sshTTY) Drain() error                { return t.reader.Close() }
func (t *sshTTY) Stop() error                 { return nil }
func (t *sshTTY) Close() error {
	t.stop.Do(func() { close(t.done) })
	_ = t.writer.Close()
	return t.reader.Close()
}
func (t *sshTTY) NotifyResize(cb func()) { t.mu.Lock(); t.resize = cb; t.mu.Unlock() }
func (t *sshTTY) WindowSize() (tcell.WindowSize, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return tcell.WindowSize{Width: min(500, max(1, t.window.Width)), Height: min(200, max(1, t.window.Height))}, nil
}
