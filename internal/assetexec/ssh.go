package assetexec

import (
	"context"
	"errors"
	"io"
	"path"
	"regexp"
	"sort"
	"strings"
	"sync"

	gossh "golang.org/x/crypto/ssh"
)

var environmentName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// PrepareCommand encodes approved cwd/env values as data, never shell syntax.
// The caller must authorize and audit the entire Request, not only Command.
func PrepareCommand(request Request) (string, error) {
	if strings.TrimSpace(request.Command) == "" || len(request.Command) > 64<<10 || strings.ContainsRune(request.Command, 0) {
		return "", errors.New("command must contain 1 to 65536 bytes without NUL")
	}
	if request.Cwd == "" && len(request.Env) == 0 {
		return request.Command, nil
	}
	if request.Cwd != "" && (!path.IsAbs(request.Cwd) || len(request.Cwd) > 4096 || strings.ContainsRune(request.Cwd, 0)) {
		return "", errors.New("cwd must be an absolute POSIX path of at most 4096 bytes")
	}
	if len(request.Env) > 64 {
		return "", errors.New("too many environment variables")
	}
	keys := make([]string, 0, len(request.Env))
	for key, value := range request.Env {
		if !environmentName.MatchString(key) || len(value) > 4096 || strings.ContainsRune(value, 0) {
			return "", errors.New("invalid environment variable")
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var command strings.Builder
	if request.Cwd != "" {
		command.WriteString("cd -- " + shellQuote(request.Cwd) + " && ")
	}
	command.WriteString("exec /usr/bin/env")
	for _, key := range keys {
		command.WriteString(" " + shellQuote(key+"="+request.Env[key]))
	}
	command.WriteString(" /bin/sh -c " + shellQuote(request.Command))
	if command.Len() > 128<<10 {
		return "", errors.New("execution parameters are too large")
	}
	return command.String(), nil
}

func shellQuote(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'" }

type outputSink struct {
	mu        sync.Mutex
	emit      Emit
	remaining int64
	err       error
	truncated bool
	cancel    context.CancelFunc
}

func (s *outputSink) write(stream string, p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return 0, s.err
	}
	n := len(p)
	if int64(n) > s.remaining {
		n = int(s.remaining)
		s.truncated = true
	}
	if n > 0 {
		if err := s.emit(stream, p[:n]); err != nil {
			s.err = err
			s.cancel()
			return 0, err
		}
		s.remaining -= int64(n)
	}
	if n < len(p) {
		s.err = ErrOutputLimit
		s.cancel()
		return n, s.err
	}
	return n, nil
}

type outputWriter struct {
	sink   *outputSink
	stream string
}

func (w outputWriter) Write(p []byte) (int, error) { return w.sink.write(w.stream, p) }

func (c *Connection) executeSSH(ctx context.Context, request Request, emit Emit) (Result, error) {
	command, err := PrepareCommand(request)
	if err != nil {
		return Result{}, err
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	stop := context.AfterFunc(ctx, func() {
		// Closing the transport also bounds channel-open/request handshakes.
		// It is a stop request, never evidence that the remote process exited.
		c.closed.Store(true)
		_ = c.ssh.Close()
	})
	defer stop()
	session, err := c.ssh.AcquireSession()
	if err != nil {
		return Result{ContextLost: true}, err
	}
	defer func() { _ = session.Close(); c.ssh.ReleaseSession(session) }()
	stdout, err := session.StdoutPipe()
	if err != nil {
		return Result{}, err
	}
	stderr, err := session.StderrPipe()
	if err != nil {
		return Result{}, err
	}
	sink := &outputSink{emit: emit, remaining: c.opts.MaxOutputBytes, cancel: cancel}
	result := Result{Dispatched: true}
	if err = session.Start(command); err != nil {
		result.ContextLost = c.closed.Load()
		return result, err
	}
	var readers sync.WaitGroup
	readers.Add(2)
	errorsCh := make(chan error, 2)
	for _, stream := range []struct {
		name   string
		reader io.Reader
	}{{"stdout", stdout}, {"stderr", stderr}} {
		go func(name string, reader io.Reader) {
			defer readers.Done()
			_, err := io.CopyBuffer(outputWriter{sink, name}, reader, make([]byte, 32<<10))
			errorsCh <- err
		}(stream.name, stream.reader)
	}
	waitErr := session.Wait()
	readers.Wait()
	result.Truncated = sink.truncated
	result.ContextLost = c.closed.Load()
	readErr := errors.Join(<-errorsCh, <-errorsCh)
	exitCode := 0
	if waitErr != nil {
		var exitErr *gossh.ExitError
		if !errors.As(waitErr, &exitErr) {
			return result, errors.Join(waitErr, ctx.Err(), sink.err, readErr)
		}
		exitCode = exitErr.ExitStatus()
	}
	result.Completed = true
	result.ExitCode = &exitCode
	result.StopConfirmed = ctx.Err() != nil
	return result, errors.Join(waitErr, ctx.Err(), sink.err, readErr)
}
