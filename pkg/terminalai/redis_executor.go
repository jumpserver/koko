package terminalai

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/anmitsu/go-shlex"
	"github.com/go-redis/redis/v8"

	"github.com/jumpserver/koko/pkg/common"
)

const (
	maxRedisRESPBytes    = 10 * 1024 * 1024
	maxRedisRESPElements = 100000
)

type redisExecutorClient interface {
	Do(ctx context.Context, args ...interface{}) *redis.Cmd
	Ping(ctx context.Context) *redis.StatusCmd
	Close() error
}

type RedisExecutor struct {
	client    redisExecutorClient
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

func newRedisClient(config DatabaseConfig) (redisExecutorClient, error) {
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
	var tlsConfig *tls.Config
	if config.UseSSL {
		var err error
		tlsConfig, err = databaseTLSConfig(config)
		if err != nil {
			return nil, fmt.Errorf("configure Redis TLS: %w", err)
		}
	}
	address := net.JoinHostPort(config.Host, strconv.Itoa(config.Port))
	if config.ClusterMode {
		return redis.NewClusterClient(&redis.ClusterOptions{
			Addrs:        []string{address},
			Username:     config.Username,
			Password:     config.Password,
			DialTimeout:  10 * time.Second,
			ReadTimeout:  -1,
			WriteTimeout: -1,
			PoolSize:     1,
			MaxRetries:   -1,
			TLSConfig:    tlsConfig,
			ClusterSlots: common.RedisClusterSlots([]string{address}, redis.Options{
				Username:     config.Username,
				Password:     config.Password,
				DialTimeout:  10 * time.Second,
				ReadTimeout:  -1,
				WriteTimeout: -1,
				PoolSize:     1,
				MaxRetries:   -1,
				TLSConfig:    tlsConfig,
			}),
		}), nil
	}
	return redis.NewClient(&redis.Options{
		Addr:         address,
		Username:     config.Username,
		Password:     config.Password,
		DB:           database,
		DialTimeout:  10 * time.Second,
		ReadTimeout:  -1,
		WriteTimeout: -1,
		PoolSize:     1,
		MaxRetries:   -1,
		TLSConfig:    tlsConfig,
	}), nil
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
	commandArgs := make([]interface{}, len(arguments))
	for index := range arguments {
		commandArgs[index] = arguments[index]
	}
	value, err := e.client.Do(ctx, commandArgs...).Result()
	if errors.Is(err, redis.Nil) {
		value = nil
		err = nil
	}
	if err == nil {
		err = response.writeResult(value, 0)
	}
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
	return e.client.Ping(ctx).Err()
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

type redisRESPOutput struct {
	output   *boundedDatabaseOutput
	read     int64
	elements int
	values   int
}

func (o *redisRESPOutput) writeResult(value interface{}, depth int) error {
	switch value := value.(type) {
	case nil:
		return o.writeValue("(nil)")
	case string:
		return o.writeValue(value)
	case int64:
		return o.writeValue(strconv.FormatInt(value, 10))
	case error:
		if depth > 0 {
			if err := o.writeValue("(error) " + value.Error()); err != nil {
				return err
			}
		}
		return value
	case []interface{}:
		if len(value) == 0 {
			return o.writeValue("(empty array)")
		}
		if len(value) > maxRedisRESPElements-o.elements {
			return fmt.Errorf("Redis response exceeds %d element limit", maxRedisRESPElements)
		}
		o.elements += len(value)
		var firstErr error
		for _, item := range value {
			itemErr := o.writeResult(item, depth+1)
			if itemErr == nil {
				continue
			}
			var responseErr interface{ RedisError() }
			if !errors.As(itemErr, &responseErr) {
				return itemErr
			}
			if firstErr == nil {
				firstErr = itemErr
			}
		}
		return firstErr
	default:
		return fmt.Errorf("unsupported Redis response type %T", value)
	}
}

func (o *redisRESPOutput) beginValue() {
	if o.values > 0 {
		_, _ = o.output.Write([]byte("\n"))
	}
	o.values++
}

func (o *redisRESPOutput) writeValue(value string) error {
	o.read += int64(len(value))
	if o.read > maxRedisRESPBytes {
		return fmt.Errorf("Redis response exceeds 10 MiB limit")
	}
	o.beginValue()
	_, _ = o.output.Write([]byte(value))
	return nil
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
