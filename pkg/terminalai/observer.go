package terminalai

import (
	"bytes"
	"fmt"
	"strings"
	"sync"

	"github.com/LeeEirc/terminalparser"
)

const (
	maxObservedOutput          = 100 * 1024
	outputTruncatedFirstMarker = "[output truncated; showing first 100 KiB]"
	outputTruncatedLastMarker  = "[output truncated; showing last 100 KiB]"
)

type CommandResult struct {
	Command string
	Output  string
}

type Observer struct {
	mu        sync.Mutex
	terminal  *terminalparser.TerminalVT
	width     uint16
	height    uint16
	active    bool
	command   string
	prompt    string
	output    bytes.Buffer
	truncated bool
	result    chan CommandResult
}

func NewObserver(width, height int) (*Observer, error) {
	if width <= 0 || height <= 0 || width > 65535 || height > 65535 {
		return nil, fmt.Errorf("invalid terminal dimensions")
	}
	terminal, err := terminalparser.New(
		terminalparser.WithSize(uint16(width), uint16(height)),
		terminalparser.WithMaxScrollback(0),
	)
	if err != nil {
		return nil, fmt.Errorf("create terminal observer: %w", err)
	}
	return &Observer{
		terminal: terminal, width: uint16(width), height: uint16(height),
		result: make(chan CommandResult, 1),
	}, nil
}

func (o *Observer) Begin(command string) (<-chan CommandResult, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.active {
		return nil, fmt.Errorf("another command is already being observed")
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

func (o *Observer) Feed(data []byte) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if _, err := o.terminal.Write(data); err != nil || !o.active {
		return
	}
	if o.output.Len() < maxObservedOutput {
		remaining := maxObservedOutput - o.output.Len()
		if len(data) > remaining {
			data = data[:remaining]
			o.truncated = true
		}
		_, _ = o.output.Write(data)
	} else if len(data) > 0 {
		o.truncated = true
	}
	row, err := o.terminal.CursorRow()
	if err != nil || o.prompt == "" || strings.TrimSpace(row) != o.prompt {
		return
	}
	result := CommandResult{Command: o.command, Output: o.parseOutputLocked()}
	o.active = false
	o.command = ""
	o.prompt = ""
	o.output.Reset()
	o.truncated = false
	select {
	case o.result <- result:
	default:
	}
}

func (o *Observer) parseOutputLocked() string {
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
	return markObservedOutput(
		strings.TrimSpace(result.String()), o.truncated,
	)
}

func markObservedOutput(value string, truncated bool) string {
	if !truncated {
		return value
	}
	if value == "" {
		return outputTruncatedFirstMarker
	}
	return outputTruncatedFirstMarker + "\n" + value
}

func outputIsTruncated(value string) bool {
	return strings.Contains(value, "[output truncated")
}

func (o *Observer) Snapshot() string {
	o.mu.Lock()
	defer o.mu.Unlock()
	value, _ := o.terminal.String()
	return strings.TrimSpace(value)
}

func (o *Observer) Resize(width, height int) {
	if width <= 0 || height <= 0 || width > 65535 || height > 65535 {
		return
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	o.width, o.height = uint16(width), uint16(height)
	_ = o.terminal.Resize(o.width, o.height, 0, 0)
}

func (o *Observer) Cancel() {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.active = false
	o.command = ""
	o.prompt = ""
	o.output.Reset()
	o.truncated = false
}

func (o *Observer) Close() error {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.terminal.Close()
}
