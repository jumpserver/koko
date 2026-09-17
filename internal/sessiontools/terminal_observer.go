package sessiontools

import (
	"bytes"
	"fmt"
	"strings"
	"sync"

	"github.com/LeeEirc/terminalparser"
)

const (
	maxTerminalObserverOutput     = 100 * 1024
	maxTerminalObserverSnapshot   = 64 * 1024
	maxTerminalObserverWidth      = 1000
	maxTerminalObserverHeight     = 500
	terminalObserverTruncatedMark = "[output truncated; showing first 100 KiB]"
)

type TerminalCommandResult struct {
	Command string
	Output  string
}

// TerminalObserver keeps a bounded local terminal view. Feed never leaves the
// Koko process; Snapshot is exposed only through the current session tool.
type TerminalObserver struct {
	mu        sync.Mutex
	terminal  *terminalparser.TerminalVT
	width     uint16
	height    uint16
	active    bool
	command   string
	prompt    string
	output    bytes.Buffer
	truncated bool
	result    chan TerminalCommandResult
}

func NewTerminalObserver(width, height int) (*TerminalObserver, error) {
	width, height = boundedTerminalSize(width, height)
	terminal, err := terminalparser.New(
		terminalparser.WithSize(uint16(width), uint16(height)),
		terminalparser.WithMaxScrollback(200),
	)
	if err != nil {
		return nil, fmt.Errorf("create terminal observer: %w", err)
	}
	return &TerminalObserver{
		terminal: terminal, width: uint16(width), height: uint16(height),
		result: make(chan TerminalCommandResult, 1),
	}, nil
}

func (o *TerminalObserver) Begin(command string) (<-chan TerminalCommandResult, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.active {
		return nil, fmt.Errorf("another PTY tool call is active")
	}
	prompt, err := o.terminal.CursorRow()
	if err != nil {
		return nil, err
	}
	o.active = true
	o.command = strings.TrimSpace(command)
	o.prompt = strings.TrimSpace(prompt)
	o.output.Reset()
	o.truncated = false
	for len(o.result) > 0 {
		<-o.result
	}
	return o.result, nil
}

func (o *TerminalObserver) Feed(data []byte) {
	if len(data) == 0 {
		return
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if _, err := o.terminal.Write(data); err != nil || !o.active {
		return
	}
	if remaining := maxTerminalObserverOutput - o.output.Len(); remaining > 0 {
		if len(data) > remaining {
			data = data[:remaining]
			o.truncated = true
		}
		_, _ = o.output.Write(data)
	} else {
		o.truncated = true
	}
	row, err := o.terminal.CursorRow()
	if err != nil || o.prompt == "" || strings.TrimSpace(row) != o.prompt {
		return
	}
	result := TerminalCommandResult{Command: o.command, Output: o.parseOutputLocked()}
	o.resetLocked()
	select {
	case o.result <- result:
	default:
	}
}

func (o *TerminalObserver) Snapshot() string {
	o.mu.Lock()
	defer o.mu.Unlock()
	value, _ := o.terminal.String()
	value = strings.TrimSpace(value)
	if len(value) <= maxTerminalObserverSnapshot {
		return value
	}
	return "[snapshot truncated; showing last 64 KiB]\n" +
		value[len(value)-maxTerminalObserverSnapshot:]
}

func (o *TerminalObserver) Resize(width, height int) {
	width, height = boundedTerminalSize(width, height)
	o.mu.Lock()
	defer o.mu.Unlock()
	o.width, o.height = uint16(width), uint16(height)
	_ = o.terminal.Resize(o.width, o.height, 0, 0)
}

func (o *TerminalObserver) Cancel() {
	o.mu.Lock()
	o.resetLocked()
	o.mu.Unlock()
}

func (o *TerminalObserver) Close() error {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.resetLocked()
	return o.terminal.Close()
}

func (o *TerminalObserver) parseOutputLocked() string {
	rows, err := terminalparser.Parse(
		o.output.Bytes(),
		terminalparser.WithSize(o.width, o.height),
		terminalparser.WithMaxScrollback(500),
		terminalparser.WithUnwrap(true),
	)
	if err != nil {
		return ""
	}
	var result strings.Builder
	for _, row := range rows {
		row = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(row), o.prompt))
		if row == "" || row == o.command {
			continue
		}
		if result.Len() > 0 {
			result.WriteByte('\n')
		}
		result.WriteString(row)
	}
	value := strings.TrimSpace(result.String())
	if o.truncated {
		value = terminalObserverTruncatedMark + "\n" + value
	}
	return strings.TrimSpace(value)
}

func (o *TerminalObserver) resetLocked() {
	o.active = false
	o.command = ""
	o.prompt = ""
	o.output.Reset()
	o.truncated = false
}

func boundedTerminalSize(width, height int) (int, int) {
	if width <= 0 {
		width = 80
	} else if width > maxTerminalObserverWidth {
		width = maxTerminalObserverWidth
	}
	if height <= 0 {
		height = 24
	} else if height > maxTerminalObserverHeight {
		height = maxTerminalObserverHeight
	}
	return width, height
}
