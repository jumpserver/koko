package terminalai

import (
	"bytes"
	"context"
	"errors"
	"io"
	"sync"

	"github.com/jumpserver/koko/pkg/srvconn"
	gossh "golang.org/x/crypto/ssh"
)

type SSHExecutor struct {
	client *srvconn.SSHClient
}

func NewSSHExecutor(client *srvconn.SSHClient) *SSHExecutor {
	return &SSHExecutor{client: client}
}

func (e *SSHExecutor) DetectProfile(ctx context.Context) AssetProfile {
	session, err := e.client.AcquireSession()
	if err != nil {
		return AssetProfile{DetectionError: err.Error()}
	}
	defer func() {
		_ = session.Close()
		e.client.ReleaseSession(session)
	}()
	type result struct {
		output []byte
		err    error
	}
	done := make(chan result, 1)
	go func() {
		output, runErr := session.CombinedOutput(AssetProfileProbeCommand())
		done <- result{output: output, err: runErr}
	}()
	select {
	case <-ctx.Done():
		_ = session.Close()
		return AssetProfile{DetectionError: ctx.Err().Error()}
	case value := <-done:
		profile := ParseAssetProfile(string(value.output))
		if value.err != nil {
			profile.DetectionError = value.err.Error()
		}
		return profile
	}
}

func (e *SSHExecutor) Execute(
	ctx context.Context, command string, onOutput func(string),
) (string, *int, error) {
	session, err := e.client.AcquireSession()
	if err != nil {
		return "", nil, &BackgroundUnavailableError{Cause: err}
	}
	defer func() {
		_ = session.Close()
		e.client.ReleaseSession(session)
	}()
	stdout, err := session.StdoutPipe()
	if err != nil {
		return "", nil, &BackgroundUnavailableError{Cause: err}
	}
	stderr, err := session.StderrPipe()
	if err != nil {
		return "", nil, &BackgroundUnavailableError{Cause: err}
	}
	if err = session.Start(command); err != nil {
		return "", nil, &BackgroundUnavailableError{Cause: err}
	}
	buffer := &boundedOutput{onUpdate: onOutput}
	var writers sync.WaitGroup
	writers.Add(2)
	go copyOutput(&writers, buffer, stdout)
	go copyOutput(&writers, buffer, stderr)
	cancelDone := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = session.Signal(gossh.SIGINT)
			_ = session.Close()
		case <-cancelDone:
		}
	}()
	waitErr := session.Wait()
	close(cancelDone)
	writers.Wait()
	output := buffer.String()
	if ctx.Err() != nil {
		return output, nil, ctx.Err()
	}
	exitCode := 0
	if waitErr != nil {
		var exitErr *gossh.ExitError
		if errors.As(waitErr, &exitErr) {
			exitCode = exitErr.ExitStatus()
		} else {
			return output, nil, &BackgroundUnavailableError{Cause: waitErr}
		}
	}
	return output, &exitCode, nil
}

func (e *SSHExecutor) Close() error {
	return nil
}

func copyOutput(wg *sync.WaitGroup, target io.Writer, source io.Reader) {
	defer wg.Done()
	_, _ = io.Copy(target, source)
}

type boundedOutput struct {
	mu        sync.Mutex
	buffer    bytes.Buffer
	truncated bool
	onUpdate  func(string)
	emitted   int
}

func (b *boundedOutput) Write(value []byte) (int, error) {
	b.mu.Lock()
	original := len(value)
	if b.buffer.Len()+len(value) > maxObservedOutput {
		combined := append(append([]byte(nil), b.buffer.Bytes()...), value...)
		combined = combined[len(combined)-maxObservedOutput:]
		b.buffer.Reset()
		_, _ = b.buffer.Write(combined)
		b.truncated = true
	} else {
		_, _ = b.buffer.Write(value)
	}
	var update string
	if b.onUpdate != nil && b.emitted < maxObservedOutput {
		remaining := maxObservedOutput - b.emitted
		if len(value) > remaining {
			update = string(value[:remaining])
		} else {
			update = string(value)
		}
		b.emitted += len(update)
	}
	b.mu.Unlock()
	if update != "" {
		b.onUpdate(update)
	}
	return original, nil
}

func (b *boundedOutput) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	value := b.buffer.String()
	if b.truncated {
		return outputTruncatedLastMarker + "\n" + value
	}
	return value
}
