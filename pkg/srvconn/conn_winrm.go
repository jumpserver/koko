package srvconn

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"

	"github.com/charmbracelet/x/ansi"
	"golang.org/x/term"
)

type winRMInput struct {
	data []byte
	user string
}

type WinRMConnection struct {
	Client        *WinRMClient
	terminal      *term.Terminal
	input         chan winRMInput
	output        *io.PipeReader
	writer        *io.PipeWriter
	pending       []byte
	user          string
	ctx           context.Context
	cancel        context.CancelFunc
	activeMu      sync.Mutex
	terminalMu    sync.Mutex
	prompt        string
	width, height int
	activeCancel  context.CancelFunc
	interrupt     chan struct{}
	once          sync.Once
	started       sync.Once
	isStarted     atomic.Bool
	done          chan struct{}
}

func NewWinRMConnection(config WinRMConfig) (*WinRMConnection, error) {
	client, err := NewWinRMClient(config)
	if err != nil {
		return nil, err
	}
	reader, writer := io.Pipe()
	ctx, cancel := context.WithCancel(client.ctx)
	c := &WinRMConnection{Client: client, input: make(chan winRMInput, 16), interrupt: make(chan struct{}, 1),
		output: reader, writer: writer, ctx: ctx, cancel: cancel, done: make(chan struct{})}
	c.resetTerminal()
	client.OnLocation = c.setLocation
	client.OutputWidth = c.outputWidth
	// Initialize the prompt from this runspace before accepting user input.
	promptCtx, promptCancel := context.WithTimeout(ctx, 30*time.Second)
	defer promptCancel()
	if err := client.Execute(promptCtx, "", io.Discard); err != nil {
		_ = c.Close()
		return nil, err
	}
	return c, nil
}

func (c *WinRMConnection) Start(handle func(context.Context, string, string, io.Writer) error) {
	c.started.Do(func() {
		c.isStarted.Store(true)
		go func() {
			defer close(c.done)
			defer func() { _ = c.writer.Close() }()
			c.terminal.SetBracketedPasteMode(true)
			for c.ctx.Err() == nil {
				command, err := c.terminal.ReadLine()
				if err != nil && !errors.Is(err, term.ErrPasteIndicator) {
					if c.ctx.Err() != nil {
						return
					}
					// Ctrl-C clears the local input; Ctrl-D ends the session.
					if errors.Is(err, errWinRMInterrupt) {
						c.resetTerminal()
						_, _ = io.WriteString(c.writer, "^C\r\n")
						continue
					}
					return
				}
				command = strings.TrimSpace(command)
				if command == "" {
					continue
				}
				user := c.user
				ctx, cancel := context.WithTimeout(c.ctx, 10*time.Minute)
				c.activeMu.Lock()
				c.activeCancel = cancel
				c.activeMu.Unlock()
				err = handle(ctx, command, user, c.terminal)
				cancel()
				c.activeMu.Lock()
				c.activeCancel = nil
				c.activeMu.Unlock()
				if err != nil {
					_, _ = io.WriteString(c.terminal, err.Error()+"\n\n")
				}
			}
		}()
	})
}

func (c *WinRMConnection) Confirm(ctx context.Context, prompt string) bool {
	stop := context.AfterFunc(ctx, func() {
		select {
		case c.interrupt <- struct{}{}:
		default:
		}
	})
	defer stop()
	c.terminal.SetPrompt(prompt + " [y/N] ")
	defer func() { c.terminal.SetPrompt(c.Prompt()) }()
	answer, err := c.terminal.ReadLine()
	if errors.Is(err, errWinRMInterrupt) {
		c.resetTerminal()
	}
	return err == nil && strings.EqualFold(strings.TrimSpace(answer), "y")
}

func (c *WinRMConnection) Read(data []byte) (int, error)  { return c.output.Read(data) }
func (c *WinRMConnection) Write(data []byte) (int, error) { return c.WriteInput(data, "") }

// WriteOutput feeds the normal replay and room stream while preserving a partially typed line.
func (c *WinRMConnection) WriteOutput(ctx context.Context, data []byte) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	c.terminalMu.Lock()
	terminal := c.terminal
	c.terminalMu.Unlock()
	stop := context.AfterFunc(ctx, func() {
		// A stalled output consumer must not keep a cancelled command alive.
		c.cancel()
		_ = c.writer.CloseWithError(ctx.Err())
	})
	defer stop()
	return terminal.Write(data)
}

func (c *WinRMConnection) WriteInput(data []byte, user string) (int, error) {
	if len(data) == 0 {
		return 0, nil
	}
	if bytes.IndexByte(data, 3) >= 0 {
		c.activeMu.Lock()
		cancel := c.activeCancel
		c.activeMu.Unlock()
		if cancel != nil {
			cancel()
			return len(data), nil
		}
		if len(data) == 1 {
			select {
			case c.interrupt <- struct{}{}:
			default:
			}
			return len(data), nil
		}
	}
	written := 0
	for written < len(data) {
		n := min(1024, len(data)-written)
		select {
		case c.input <- winRMInput{data: append([]byte(nil), data[written:written+n]...), user: user}:
			written += n
		case <-c.ctx.Done():
			return written, io.ErrClosedPipe
		}
	}
	return written, nil
}

func (c *WinRMConnection) SetWinSize(width, height int) error {
	if width <= 0 || height <= 0 || width > 65535 || height > 65535 {
		return errors.New("invalid WinRM terminal size")
	}
	c.terminalMu.Lock()
	defer c.terminalMu.Unlock()
	c.width, c.height = width, height
	return c.terminal.SetSize(width, height)
}

func (c *WinRMConnection) resetTerminal() {
	c.terminalMu.Lock()
	defer c.terminalMu.Unlock()
	if c.prompt == "" {
		c.prompt = "PS> "
	}
	c.terminal = term.NewTerminal(&winRMTerminalIO{connection: c}, c.prompt)
	if c.width > 0 && c.height > 0 {
		_ = c.terminal.SetSize(c.width, c.height)
	}
}

func (c *WinRMConnection) Prompt() string {
	c.terminalMu.Lock()
	defer c.terminalMu.Unlock()
	return c.prompt
}

func (c *WinRMConnection) outputWidth() int {
	c.terminalMu.Lock()
	defer c.terminalMu.Unlock()
	if c.width == 0 {
		return 120
	}
	return c.width
}

func (c *WinRMConnection) setLocation(ctx context.Context, path string) {
	if path == "" || len(path) > 32768 || strings.ContainsFunc(path, unicode.IsControl) {
		return
	}
	c.terminalMu.Lock()
	c.prompt = "PS " + path + "> "
	c.terminal.SetPrompt(c.prompt)
	c.terminalMu.Unlock()
	if c.isStarted.Load() {
		_, _ = c.WriteOutput(ctx, nil)
	}
}
func (c *WinRMConnection) KeepAlive() error { return c.ctx.Err() }

func (c *WinRMConnection) Close() error {
	c.once.Do(func() {
		c.cancel()
		_ = c.output.Close()
		_ = c.writer.Close()
		_ = c.Client.Close()
		if c.isStarted.Load() {
			<-c.done
		}
	})
	return nil
}

var errWinRMInterrupt = errors.New("WinRM input interrupted")

type winRMTerminalIO struct {
	connection *WinRMConnection
	buffer     []byte
	literal    bool
	paste      bool
	state      byte
	sequence   [6]byte
	sequenceN  int
}

// Use x/term's existing paste path for text, including clients that send plain
// input. Its normal key path silently stops at 4096 runes. Keep editing keys and
// escape sequences outside these markers, and preserve actual bracketed pastes.
func (t *winRMTerminalIO) bufferInput(data []byte) {
	endLiteral := func() {
		if t.literal {
			t.buffer = append(t.buffer, "\x1b[201~"...)
			t.literal = false
		}
	}
	for i, b := range data {
		if t.state != ansi.NormalState || b == 27 {
			endLiteral()
			if t.state == ansi.NormalState {
				t.sequenceN = 0
			}
			if t.sequenceN < len(t.sequence) {
				t.sequence[t.sequenceN] = b
			}
			t.sequenceN = min(t.sequenceN+1, len(t.sequence)+1)
			_, _, _, t.state = ansi.DecodeSequence(data[i:i+1], t.state, nil)
			if t.state == ansi.NormalState && t.sequenceN == len(t.sequence) {
				switch string(t.sequence[:]) {
				case "\x1b[200~":
					t.paste = true
				case "\x1b[201~":
					t.paste = false
				}
			}
		} else if !t.paste {
			if b < 32 || b == 127 {
				endLiteral()
			} else if !t.literal {
				t.buffer = append(t.buffer, "\x1b[200~"...)
				t.literal = true
			}
		}
		t.buffer = append(t.buffer, b)
	}
}

func (t *winRMTerminalIO) Write(data []byte) (int, error) { return t.connection.writer.Write(data) }
func (t *winRMTerminalIO) Read(data []byte) (int, error) {
	if len(t.buffer) > 0 {
		n := copy(data, t.buffer)
		t.buffer = t.buffer[n:]
		return n, nil
	}
	c := t.connection
	for len(c.pending) == 0 {
		select {
		case input := <-c.input:
			c.pending = input.data
			c.user = input.user
		case <-c.ctx.Done():
			return 0, io.EOF
		case <-c.interrupt:
			return 0, errWinRMInterrupt
		}
	}
	if c.pending[0] == 3 {
		c.pending = c.pending[1:]
		return 0, errWinRMInterrupt
	}
	pending := c.pending
	if index := bytes.IndexByte(pending, 3); index >= 0 {
		pending = pending[:index]
	}
	t.bufferInput(pending)
	c.pending = c.pending[len(pending):]
	n := copy(data, t.buffer)
	t.buffer = t.buffer[n:]
	return n, nil
}
