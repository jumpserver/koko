// Package tui provides the SSH screen and embedded terminal used by Koko's TUI.
package tui

import (
	"encoding/base64"
	"io"
	"strings"
	"sync"

	"github.com/gdamore/tcell/v2"
	"github.com/gdamore/tcell/v2/terminfo"
	_ "github.com/gdamore/tcell/v2/terminfo/extended"
	"github.com/gliderlabs/ssh"
)

// sshTTY owns the active TUI reader. Draining the screen releases tcell's reader
// without closing the SSH channel before it restores terminal modes; callers may
// route that reader through a longer-lived session when switching interfaces.
type sshTTY struct {
	session ssh.Session
	reader  *io.PipeReader
	writer  *io.PipeWriter
	mu      sync.Mutex
	output  sync.Mutex
	route   sync.RWMutex
	window  ssh.Window
	resize  func()
	forward func([]byte) bool
	start   sync.Once
	stop    sync.Once
	done    chan struct{}
}

func NewSSHScreen(session ssh.Session, windows <-chan ssh.Window) (tcell.Screen, error) {
	pty, _, _ := session.Pty()
	clientVersion := session.Context().ClientVersion()
	term := sshTerminfoName(pty.Term, clientVersion)
	ti, err := terminfo.LookupTerminfo(term)
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
	if IsXShellClient(clientVersion) {
		// Xshell obeys DEC cursor blinking mode independently from DECSCUSR.
		// xterm's ShowCursor disables that mode, leaving its configured green
		// block fixed over the character even after requesting a blinking bar.
		networkInfo.ShowCursor = "\x1b[?12h\x1b[?25h"
		networkInfo.CursorDefault = "\x1b[0 q"
		networkInfo.CursorBlinkingBlock = "\x1b[1 q"
		networkInfo.CursorSteadyBlock = "\x1b[2 q"
		networkInfo.CursorBlinkingUnderline = "\x1b[3 q"
		networkInfo.CursorSteadyUnderline = "\x1b[4 q"
		networkInfo.CursorBlinkingBar = "\x1b[5 q"
		networkInfo.CursorSteadyBar = "\x1b[6 q"
	}
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
	cursorReset := networkInfo.CursorDefault
	if IsXShellClient(clientVersion) {
		cursorReset += "\x1b[?12l"
	}
	if cursorReset == "" && (ti.Mouse != "" || ti.XTermLike) {
		cursorReset = "\x1b[0 q"
	}
	return &sshScreen{Screen: screen, tty: tty, output: tty, cursorReset: cursorReset}, nil
}

func sshTerminfoName(term, clientVersion string) string {
	if strings.EqualFold(term, "xterm") && IsXShellClient(clientVersion) {
		return "xterm-256color"
	}
	return term
}

func IsXShellClient(clientVersion string) bool {
	clientVersion = strings.ToLower(clientVersion)
	return strings.Contains(clientVersion, "xshell") || strings.Contains(clientVersion, "netsarang") || strings.Contains(clientVersion, "nsssh")
}

// SetScreen calls Init itself and ignores errors; initialize once explicitly.
type sshScreen struct {
	tcell.Screen
	once        sync.Once
	err         error
	fini        sync.Once
	tty         *sshTTY
	output      io.Writer
	cursorReset string
}

func (s *sshScreen) Init() error { s.once.Do(func() { s.err = s.Screen.Init() }); return s.err }

func (s *sshScreen) Fini() {
	s.fini.Do(func() {
		if s.err != nil {
			// tcell may leave its quit channel uninitialized when Init fails,
			// making Screen.Fini unsafe. Still stop our SSH input and resize
			// goroutines so a line-oriented fallback can take ownership.
			_ = s.tty.Close()
			return
		}
		s.Screen.Fini()
		// tcell may not emit a pending default style after hiding the cursor.
		// Restore it while the SSH channel is still writable.
		if s.cursorReset != "" {
			_, _ = io.WriteString(s.output, s.cursorReset)
		}
	})
}

func (s *sshScreen) SetClipboard(data []byte) {
	if len(data) == 0 {
		return
	}
	// Terminfo may omit clipboard support even when the SSH client accepts OSC 52.
	encoded := make([]byte, 0, base64.StdEncoding.EncodedLen(len(data))+9)
	encoded = append(encoded, "\x1b]52;c;"...)
	encoded = base64.StdEncoding.AppendEncode(encoded, data)
	encoded = append(encoded, "\x1b\\"...)
	_, _ = s.output.Write(encoded)
}

func (t *sshTTY) Start() error {
	t.start.Do(func() {
		go func() {
			buf := make([]byte, 8192)
			for {
				n, err := t.session.Read(buf)
				if n > 0 {
					t.route.RLock()
					forward := t.forward
					t.route.RUnlock()
					if forward != nil {
						forward(buf[:n])
					} else if _, writeErr := t.writer.Write(buf[:n]); writeErr != nil {
						err = writeErr
					}
				}
				if err != nil {
					_ = t.writer.CloseWithError(err)
					return
				}
			}
		}()
	})
	return nil
}
func (t *sshTTY) Read(b []byte) (int, error) { return t.reader.Read(b) }
func (t *sshTTY) Write(b []byte) (int, error) {
	t.route.RLock()
	defer t.route.RUnlock()
	if t.forward != nil {
		return len(b), nil
	}
	return t.writeOutput(b)
}
func (t *sshTTY) writeOutput(b []byte) (int, error) {
	t.output.Lock()
	defer t.output.Unlock()
	return t.session.Write(b)
}
func (t *sshTTY) Drain() error { return t.reader.Close() }
func (t *sshTTY) Stop() error  { return nil }
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

func (t *sshTTY) beginPassthrough(forward func([]byte) bool) bool {
	if forward == nil {
		return false
	}
	t.route.Lock()
	defer t.route.Unlock()
	if t.forward != nil {
		return false
	}
	t.forward = forward
	return true
}

func (t *sshTTY) endPassthrough() {
	t.route.Lock()
	t.forward = nil
	t.route.Unlock()
}

func (s *sshScreen) beginPassthrough(forward func([]byte) bool) bool {
	return s.tty != nil && s.tty.beginPassthrough(forward)
}

func (s *sshScreen) endPassthrough() {
	if s.tty != nil {
		s.tty.endPassthrough()
	}
}

func (s *sshScreen) writePassthrough(p []byte) (int, error) {
	if s.tty == nil {
		return 0, io.ErrClosedPipe
	}
	return s.tty.writeOutput(p)
}
