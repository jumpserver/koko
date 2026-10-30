package assetexec

import (
	"context"
	"errors"
	"fmt"

	"github.com/jumpserver/koko/internal/sessiontools"
	"go.mongodb.org/mongo-driver/mongo"
)

func (c *Connection) executeDatabaseCommand(ctx context.Context, command string, emit Emit) (Result, error) {
	if _, err := PrepareCommand(Request{Command: command}); err != nil {
		return Result{}, err
	}
	constraints, err := sessiontools.ProtocolCommandValidator(c.protocol)(command)
	if err != nil {
		return Result{}, err
	}
	if !constraints.BackgroundEligible {
		return Result{}, fmt.Errorf("%w: session-dependent or blocking commands require open_terminal", ErrUnsupported)
	}
	ctx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(ctx, func() { _ = c.Close() })
	defer func() { stop(); cancel() }()
	c.dbState.transport.ResetReadLimit()
	result := Result{Dispatched: true}
	output, exitCode, err := c.executor.Execute(ctx, command, nil)
	result.ExitCode = exitCode
	var redisErr interface{ RedisError() }
	var mongoErr mongo.CommandError
	result.Completed = err == nil || errors.As(err, &redisErr) || errors.As(err, &mongoErr)
	if observer, ok := c.executor.(interface{ OutputTruncated() bool }); ok {
		result.Truncated = observer.OutputTruncated()
	}
	sink := &outputSink{emit: emit, remaining: c.opts.MaxOutputBytes, cancel: cancel}
	_, outputErr := sink.write("stdout", []byte(output))
	result.Truncated = result.Truncated || sink.truncated
	if ctx.Err() != nil || !result.Completed {
		_ = c.Close()
	}
	result.ContextLost = c.closed.Load()
	return result, errors.Join(err, outputErr, ctx.Err())
}
