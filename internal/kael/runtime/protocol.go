package runtime

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"sync"
)

const CodexMinimumVersion = "0.153.2"
const maxProtocolBytes = 8 * 1024 * 1024

type rpcMessage struct {
	ID     json.RawMessage `json:"id,omitempty"`
	Method string          `json:"method,omitempty"`
	Params json.RawMessage `json:"params,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  json.RawMessage `json:"error,omitempty"`
}

type rpcProcess struct {
	cmd      *exec.Cmd
	input    io.WriteCloser
	messages chan rpcMessage
	done     chan struct{}
	once     sync.Once
	sequence int
	pending  []rpcMessage
}

func startProcess(binary, cwd string, env []string) (*rpcProcess, error) {
	cmd := exec.Command(binary, "app-server", "--listen", "stdio://")
	cmd.Dir, cmd.Env, cmd.Stderr = cwd, env, io.Discard
	isolateProcess(cmd)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return nil, err
	}
	if err = cmd.Start(); err != nil {
		_ = stdin.Close()
		_ = stdout.Close()
		return nil, fmt.Errorf("start Codex app-server: %w", err)
	}
	p := &rpcProcess{cmd: cmd, input: stdin, messages: make(chan rpcMessage, 256), done: make(chan struct{})}
	go func() {
		defer close(p.messages)
		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(make([]byte, 64*1024), maxProtocolBytes)
		for scanner.Scan() {
			var msg rpcMessage
			if json.Unmarshal(scanner.Bytes(), &msg) != nil {
				return
			}
			select {
			case p.messages <- msg:
			case <-p.done:
				return
			}
		}
	}()
	return p, nil
}

func (p *rpcProcess) close() {
	p.once.Do(func() {
		close(p.done)
		_ = p.input.Close()
		killProcessTree(p.cmd)
		_ = p.cmd.Wait()
	})
}

func (p *rpcProcess) write(value any) error {
	encoded, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if len(encoded) > maxProtocolBytes {
		return fmt.Errorf("Codex request exceeds protocol size limit")
	}
	_, err = p.input.Write(append(encoded, '\n'))
	return err
}

func (p *rpcProcess) receive(ctx context.Context) (rpcMessage, error) {
	select {
	case <-ctx.Done():
		return rpcMessage{}, ctx.Err()
	case value, ok := <-p.messages:
		if !ok {
			return rpcMessage{}, fmt.Errorf("Codex app-server closed its event stream")
		}
		return value, nil
	}
}

func (p *rpcProcess) next(ctx context.Context) (rpcMessage, error) {
	if len(p.pending) > 0 {
		msg := p.pending[0]
		p.pending = p.pending[1:]
		return msg, nil
	}
	return p.receive(ctx)
}

func (p *rpcProcess) call(ctx context.Context, method string, params any, result any) error {
	p.sequence++
	id := p.sequence
	if err := p.write(map[string]any{"id": id, "method": method, "params": params}); err != nil {
		return err
	}
	for {
		msg, err := p.receive(ctx)
		if err != nil {
			return err
		}
		if msg.Method == "" && string(msg.ID) == strconv.Itoa(id) {
			// Upstream error bodies may include credentials or sensitive tool arguments.
			if len(msg.Error) > 0 && string(msg.Error) != "null" {
				return fmt.Errorf("Codex rejected %s; check pinned runtime and provider configuration", method)
			}
			if result != nil {
				return json.Unmarshal(msg.Result, result)
			}
			return nil
		}
		if len(p.pending) >= 256 {
			return fmt.Errorf("Codex initialization event queue exceeded limit")
		}
		p.pending = append(p.pending, msg)
	}
}
