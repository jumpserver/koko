package terminalai

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/anmitsu/go-shlex"
	"github.com/mediocregopher/radix/v3"
	"github.com/mediocregopher/radix/v3/resp"
	"github.com/mediocregopher/radix/v3/resp/resp2"
)

const (
	maxRedisRESPBytes    = 10 * 1024 * 1024
	maxRedisRESPElements = 100000
	maxRedisRESPLine     = 64 * 1024
)

type RedisExecutor struct {
	client    radix.Client
	executeMu sync.Mutex
	closeOnce sync.Once
}

func NewRedisExecutor(
	ctx context.Context,
	config DatabaseConfig,
) (*RedisExecutor, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	client, err := newRedisClient(config)
	if err != nil {
		return nil, fmt.Errorf("initialize Redis background connection: %w", err)
	}
	executor := &RedisExecutor{client: client}
	if err = executor.ping(ctx); err != nil {
		_ = executor.Close()
		return nil, fmt.Errorf("initialize Redis background connection: %w", err)
	}
	return executor, nil
}

func newRedisClient(config DatabaseConfig) (radix.Client, error) {
	database := 0
	if value := strings.TrimSpace(config.Database); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil || parsed < 0 {
			return nil, fmt.Errorf("invalid Redis database %q", config.Database)
		}
		database = parsed
	}
	if config.ClusterMode && database != 0 {
		return nil, fmt.Errorf("Redis cluster mode supports only database 0")
	}
	dialOptions := []radix.DialOpt{radix.DialTimeout(10 * time.Second)}
	if config.Username != "" {
		dialOptions = append(
			dialOptions, radix.DialAuthUser(config.Username, config.Password),
		)
	} else if config.Password != "" {
		dialOptions = append(dialOptions, radix.DialAuthPass(config.Password))
	}
	if database != 0 {
		dialOptions = append(dialOptions, radix.DialSelectDB(database))
	}
	if config.UseSSL {
		tlsConfig, err := databaseTLSConfig(config)
		if err != nil {
			return nil, fmt.Errorf("configure Redis TLS: %w", err)
		}
		dialOptions = append(dialOptions, radix.DialUseTLS(tlsConfig))
	}
	connection := func(network, address string) (radix.Conn, error) {
		return radix.Dial(network, address, dialOptions...)
	}
	pool := func(network, address string) (radix.Client, error) {
		return radix.NewPool(network, address, 1, radix.PoolConnFunc(connection))
	}
	address := net.JoinHostPort(config.Host, strconv.Itoa(config.Port))
	if config.ClusterMode {
		return radix.NewCluster(
			[]string{address}, radix.ClusterPoolFunc(pool),
		)
	}
	return pool("tcp", address)
}

func (e *RedisExecutor) Execute(
	ctx context.Context,
	command string,
	onOutput func(string),
) (string, *int, error) {
	arguments, err := parseRedisCommand(command)
	if err != nil {
		return "", nil, err
	}
	if !redisBackgroundEligible(arguments) {
		return "", nil, fmt.Errorf(
			"session-oriented or blocking Redis commands require the active PTY",
		)
	}
	e.executeMu.Lock()
	defer e.executeMu.Unlock()
	response := &redisRESPOutput{output: &boundedDatabaseOutput{}}
	action := radix.Cmd(response, arguments[0], arguments[1:]...)
	err = e.client.Do(redisActionWithContext(ctx, action))
	output := strings.TrimSpace(response.output.String())
	if err != nil && output == "" && ctx.Err() == nil {
		output = err.Error()
	}
	if output != "" && onOutput != nil {
		onOutput(output)
	}
	if ctx.Err() != nil {
		return output, nil, ctx.Err()
	}
	if err != nil {
		healthCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if healthErr := e.ping(healthCtx); healthErr != nil {
			return output, nil, &BackgroundUnavailableError{Cause: healthErr}
		}
	}
	return output, nil, err
}

func (e *RedisExecutor) ping(ctx context.Context) error {
	return e.client.Do(redisActionWithContext(ctx, radix.Cmd(nil, "PING")))
}

func (e *RedisExecutor) Close() error {
	var err error
	e.closeOnce.Do(func() {
		if e.client != nil {
			err = e.client.Close()
		}
	})
	return err
}

type redisContextAction struct {
	ctx    context.Context
	action radix.Action
}

func redisActionWithContext(ctx context.Context, action radix.Action) redisContextAction {
	return redisContextAction{ctx: ctx, action: action}
}

func (a redisContextAction) Keys() []string {
	return a.action.Keys()
}

func (a redisContextAction) ClusterCanRetry() bool {
	retry, ok := a.action.(radix.ClusterCanRetryAction)
	return ok && retry.ClusterCanRetry()
}

func (a redisContextAction) Run(connection radix.Conn) error {
	if err := a.ctx.Err(); err != nil {
		return err
	}
	deadline := time.Time{}
	if value, ok := a.ctx.Deadline(); ok {
		deadline = value
	}
	if err := connection.NetConn().SetDeadline(deadline); err != nil {
		return err
	}
	cancelled := make(chan struct{})
	stop := context.AfterFunc(a.ctx, func() {
		_ = connection.NetConn().SetDeadline(time.Now())
		close(cancelled)
	})
	err := a.action.Run(connection)
	if !stop() {
		<-cancelled
	}
	_ = connection.NetConn().SetDeadline(time.Time{})
	if contextErr := a.ctx.Err(); contextErr != nil {
		return contextErr
	}
	return err
}

type redisRESPOutput struct {
	output   *boundedDatabaseOutput
	read     int64
	elements int
	values   int
}

func (o *redisRESPOutput) UnmarshalRESP(reader *bufio.Reader) error {
	return o.readValue(reader, 0)
}

func (o *redisRESPOutput) readValue(reader *bufio.Reader, depth int) error {
	prefix, err := reader.ReadByte()
	if err != nil {
		return err
	}
	o.read++
	line, err := o.readLine(reader)
	if err != nil {
		return err
	}
	switch prefix {
	case '+':
		o.writeValue(line)
		return nil
	case '-':
		responseErr := resp2.Error{E: errors.New(string(line))}
		if depth > 0 {
			o.writeValue(append([]byte("(error) "), line...))
		}
		return responseErr
	case ':':
		if _, err = strconv.ParseInt(string(line), 10, 64); err != nil {
			return fmt.Errorf("invalid Redis integer response: %w", err)
		}
		o.writeValue(line)
		return nil
	case '$':
		length, parseErr := strconv.ParseInt(string(line), 10, 64)
		if parseErr != nil || length < -1 {
			return fmt.Errorf("invalid Redis bulk response length %q", line)
		}
		if length == -1 {
			o.writeValue([]byte("(nil)"))
			return nil
		}
		if o.read+length+2 > maxRedisRESPBytes {
			return fmt.Errorf("Redis response exceeds 10 MiB limit")
		}
		o.beginValue()
		if _, err = io.CopyN(o.output, reader, length); err != nil {
			return err
		}
		o.read += length
		return o.readTerminator(reader)
	case '*':
		length, parseErr := strconv.ParseInt(string(line), 10, 32)
		if parseErr != nil || length < -1 {
			return fmt.Errorf("invalid Redis array response length %q", line)
		}
		if length == -1 {
			o.writeValue([]byte("(nil)"))
			return nil
		}
		if length == 0 {
			o.writeValue([]byte("(empty array)"))
			return nil
		}
		if length > int64(maxRedisRESPElements-o.elements) {
			return fmt.Errorf("Redis response exceeds %d element limit", maxRedisRESPElements)
		}
		o.elements += int(length)
		var firstErr error
		for index := int64(0); index < length; index++ {
			itemErr := o.readValue(reader, depth+1)
			if itemErr == nil {
				continue
			}
			var responseErr resp2.Error
			if !errors.As(itemErr, &responseErr) {
				return itemErr
			}
			if firstErr == nil {
				firstErr = itemErr
			}
		}
		if firstErr != nil {
			return resp.ErrDiscarded{Err: firstErr}
		}
		return nil
	default:
		return fmt.Errorf("unsupported Redis RESP prefix %q", prefix)
	}
}

func (o *redisRESPOutput) readLine(reader *bufio.Reader) ([]byte, error) {
	line := make([]byte, 0, 32)
	for {
		fragment, err := reader.ReadSlice('\n')
		o.read += int64(len(fragment))
		if o.read > maxRedisRESPBytes {
			return nil, fmt.Errorf("Redis response exceeds 10 MiB limit")
		}
		if len(line)+len(fragment) > maxRedisRESPLine {
			return nil, fmt.Errorf("Redis response line exceeds 64 KiB limit")
		}
		line = append(line, fragment...)
		if err == bufio.ErrBufferFull {
			continue
		}
		if err != nil {
			return nil, err
		}
		break
	}
	if len(line) < 2 || line[len(line)-2] != '\r' {
		return nil, fmt.Errorf("invalid Redis RESP line terminator")
	}
	return line[:len(line)-2], nil
}

func (o *redisRESPOutput) readTerminator(reader *bufio.Reader) error {
	var terminator [2]byte
	if _, err := io.ReadFull(reader, terminator[:]); err != nil {
		return err
	}
	o.read += 2
	if terminator != [2]byte{'\r', '\n'} {
		return fmt.Errorf("invalid Redis bulk response terminator")
	}
	return nil
}

func (o *redisRESPOutput) beginValue() {
	if o.values > 0 {
		_, _ = o.output.Write([]byte("\n"))
	}
	o.values++
}

func (o *redisRESPOutput) writeValue(value []byte) {
	o.beginValue()
	_, _ = o.output.Write(value)
}

func parseRedisCommand(command string) ([]string, error) {
	arguments, err := shlex.Split(command, true)
	if err != nil {
		return nil, fmt.Errorf("invalid Redis command: %w", err)
	}
	if len(arguments) == 0 {
		return nil, fmt.Errorf("model generated an empty Redis command")
	}
	name := arguments[0]
	if name == "" {
		return nil, fmt.Errorf("model generated an empty Redis command")
	}
	for _, char := range name {
		if (char < 'a' || char > 'z') && (char < 'A' || char > 'Z') &&
			(char < '0' || char > '9') && char != '-' && char != '_' && char != '.' {
			return nil, fmt.Errorf("invalid Redis command name %q", name)
		}
	}
	return arguments, nil
}

func redisBackgroundEligible(arguments []string) bool {
	if len(arguments) == 0 {
		return false
	}
	command := strings.ToUpper(arguments[0])
	switch command {
	case "AUTH", "HELLO", "SELECT", "QUIT", "RESET",
		"MULTI", "EXEC", "DISCARD", "WATCH", "UNWATCH",
		"SUBSCRIBE", "PSUBSCRIBE", "SSUBSCRIBE",
		"UNSUBSCRIBE", "PUNSUBSCRIBE", "SUNSUBSCRIBE",
		"MONITOR", "SYNC", "PSYNC",
		"BLPOP", "BRPOP", "BRPOPLPUSH", "BLMOVE",
		"BZPOPMIN", "BZPOPMAX", "BZMPOP":
		return false
	case "XREAD", "XREADGROUP":
		for _, argument := range arguments[1:] {
			if strings.EqualFold(argument, "BLOCK") {
				return false
			}
		}
	}
	return true
}
