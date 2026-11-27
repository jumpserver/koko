package nosql

import (
	"bufio"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

type redisSession struct {
	options *redis.Options
	client  *redis.Client
	conn    *redis.Conn
	cluster *redis.ClusterClient
	json    bool
	multi   bool
	scans   map[uint64]clusterScanState
}

func redisOptions(u *url.URL) (*redis.Options, bool, error) {
	copyURL := *u
	q, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return nil, false, errors.New("invalid Redis connection options")
	}
	for name, values := range q {
		if len(values) != 1 {
			return nil, false, fmt.Errorf("duplicate Redis option %s", name)
		}
	}
	cluster := false
	if q.Has("cluster") {
		cluster, err = strconv.ParseBool(q.Get("cluster"))
		if err != nil {
			return nil, false, errors.New("invalid Redis cluster option")
		}
		q.Del("cluster")
	}
	tlsValues := make(map[string]string)
	for _, key := range []string{"tls-ca", "tls-cert", "tls-key", "tls-server-name", "tls-insecure"} {
		tlsValues[key] = q.Get(key)
		q.Del(key)
	}
	copyURL.RawQuery = q.Encode()
	opts, err := redis.ParseURL(copyURL.String())
	if err != nil {
		return nil, false, errors.New("invalid Redis connection URL or options")
	}
	if opts.DB < 0 {
		return nil, false, errors.New("Redis database must be nonnegative")
	}
	if opts.Protocol != 0 && opts.Protocol != 2 {
		return nil, false, errors.New("Redis shell currently requires RESP2 (protocol=2)")
	}
	if cluster && opts.DB != 0 {
		return nil, false, errors.New("Redis Cluster supports database 0 only")
	}
	if opts.TLSConfig == nil {
		for _, value := range tlsValues {
			if value != "" {
				return nil, false, errors.New("Redis TLS options require a rediss:// URL")
			}
		}
	} else {
		config := opts.TLSConfig
		if name := tlsValues["tls-server-name"]; name != "" {
			config.ServerName = name
		}
		if value := tlsValues["tls-insecure"]; value != "" {
			config.InsecureSkipVerify, err = strconv.ParseBool(value)
			if err != nil {
				return nil, false, errors.New("invalid Redis tls-insecure option")
			}
		}
		if path := tlsValues["tls-ca"]; path != "" {
			pem, err := os.ReadFile(path)
			if err != nil {
				return nil, false, fmt.Errorf("read Redis CA: %w", err)
			}
			roots := x509.NewCertPool()
			if !roots.AppendCertsFromPEM(pem) {
				return nil, false, errors.New("Redis CA file contains no certificates")
			}
			config.RootCAs = roots
		}
		cert, key := tlsValues["tls-cert"], tlsValues["tls-key"]
		if (cert == "") != (key == "") {
			return nil, false, errors.New("Redis tls-cert and tls-key must be supplied together")
		}
		if cert != "" {
			pair, err := tls.LoadX509KeyPair(cert, key)
			if err != nil {
				return nil, false, fmt.Errorf("load Redis client certificate: %w", err)
			}
			config.Certificates = []tls.Certificate{pair}
		}
	}
	// Retrying an arbitrary shell command could repeat a successful write.
	opts.MaxRetries = -1
	opts.Protocol = 2
	opts.DisableIdentity = true
	opts.ContextTimeoutEnabled = true
	opts.PoolSize = 1
	opts.MaxActiveConns = 1
	opts.DialTimeout = 10 * time.Second
	// BLPOP/XREAD and other blocking commands are canceled by closing the
	// session connection, rather than an unrelated fixed socket timeout.
	opts.ReadTimeout = -1
	opts.WriteTimeout = 10 * time.Second
	return opts, cluster, nil
}

func openRedis(ctx context.Context, u *url.URL, jsonOutput bool) (session, error) {
	opts, clustered, err := redisOptions(u)
	if err != nil {
		return nil, err
	}
	s := &redisSession{options: opts, json: jsonOutput}
	if clustered {
		s.connectCluster()
		err = s.cluster.Ping(ctx).Err()
	} else {
		s.connect()
		err = s.conn.Ping(ctx).Err()
	}
	if err != nil {
		_ = s.Close()
		return nil, fmt.Errorf("connect Redis: %w", err)
	}
	return s, nil
}

func (s *redisSession) connectCluster() {
	opts := s.options
	s.cluster = redis.NewClusterClient(&redis.ClusterOptions{
		Addrs: []string{opts.Addr}, Username: opts.Username, Password: opts.Password,
		TLSConfig: opts.TLSConfig, Protocol: 2, MaxRetries: -1, DisableIdentity: true,
		DialTimeout: opts.DialTimeout, ReadTimeout: -1, WriteTimeout: opts.WriteTimeout,
		ContextTimeoutEnabled: true, PoolSize: 1,
		NewClient: func(options *redis.Options) *redis.Client {
			options.MaxRetries = -1
			client := redis.NewClient(options)
			client.AddHook(noRedisRetry{})
			return client
		},
	})
}

// ClusterClient retries transport errors independently of MaxRetries. Keep
// only MOVED/ASK redirects: an EOF after a write may mean the write succeeded.
// The error is intentionally opaque so the pinned driver's errors.Is/As retry
// detection cannot unwrap it back to an EOF or network timeout.
type noRedisRetry struct{}

func (noRedisRetry) DialHook(next redis.DialHook) redis.DialHook { return next }
func (noRedisRetry) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error { return withoutRedisRetry(next(ctx, cmd)) }
}
func (noRedisRetry) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return func(ctx context.Context, commands []redis.Cmder) error { return withoutRedisRetry(next(ctx, commands)) }
}
func withoutRedisRetry(err error) error {
	if err == nil || errors.Is(err, redis.Nil) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	var serverError redis.Error
	if errors.As(err, &serverError) && (strings.HasPrefix(err.Error(), "MOVED ") || strings.HasPrefix(err.Error(), "ASK ")) {
		return err
	}
	return errors.New("Redis command failed without retry: " + err.Error())
}

func (s *redisSession) connect() {
	opts := *s.options
	opts.MaxRetries = -1
	s.client = redis.NewClient(&opts)
	s.conn = s.client.Conn()
	s.multi = false
}

func (s *redisSession) Prompt() string {
	state := ""
	if s.cluster != nil {
		state = " cluster"
	}
	if s.multi {
		state += " tx"
	}
	return fmt.Sprintf("redis:%s[%d]%s> ", s.options.Addr, s.options.DB, state)
}

func (s *redisSession) Close() error {
	if s.cluster != nil {
		return s.cluster.Close()
	}
	if s.conn != nil {
		_ = s.conn.Close()
	}
	if s.client != nil {
		return s.client.Close()
	}
	return nil
}

func (s *redisSession) Execute(ctx context.Context, w io.Writer, line string) error {
	args, err := redisArgs(line)
	if err != nil || len(args) == 0 {
		return err
	}
	command := strings.ToUpper(args[0].(string))
	// Changing the response protocol makes generic command decoding unsafe.
	if command == "HELLO" || command == "RESET" || command == "SYNC" || command == "PSYNC" {
		return fmt.Errorf("%s is not supported in the interactive Redis session", command)
	}
	if command == "MONITOR" {
		if len(args) != 1 {
			return errors.New("MONITOR takes no arguments")
		}
		if s.multi {
			return errors.New("MONITOR cannot start inside MULTI")
		}
		return s.monitor(ctx, w)
	}
	if s.cluster != nil {
		switch command {
		case "SELECT", "AUTH", "MULTI", "EXEC", "DISCARD", "WATCH", "UNWATCH":
			return fmt.Errorf("%s requires a direct Redis node connection; reconnect without cluster=true", command)
		}
	}
	if command == "SUBSCRIBE" || command == "PSUBSCRIBE" || command == "SSUBSCRIBE" {
		if s.multi {
			return errors.New("subscriptions cannot start inside MULTI")
		}
		return s.subscribe(ctx, w, command, args[1:])
	}
	if s.multi && (command == "AUTH" || command == "SELECT") {
		return fmt.Errorf("%s must be executed outside MULTI", command)
	}
	if s.cluster != nil {
		client := s.cluster
		stop := context.AfterFunc(ctx, func() { _ = client.Close() })
		result, err := s.clusterExecute(ctx, command, args)
		if !stop() || ctx.Err() != nil {
			_ = client.Close()
			s.connectCluster()
			return errors.New("Redis Cluster operation canceled; connections reset")
		}
		if errors.Is(err, redis.Nil) {
			result, err = nil, nil
		}
		if err != nil {
			return err
		}
		return s.write(w, result)
	}
	client := s.client
	stop := context.AfterFunc(ctx, func() { _ = client.Close() })
	result, err := s.conn.Do(ctx, args...).Result()
	if !stop() || ctx.Err() != nil {
		_ = s.Close()
		s.connect()
		return errors.New("Redis operation canceled; connection reset and transaction discarded")
	}
	if errors.Is(err, redis.Nil) {
		result, err = nil, nil
	}
	if err != nil {
		var serverErr redis.Error
		if !errors.As(err, &serverErr) {
			_ = s.Close()
			s.connect()
			return fmt.Errorf("Redis connection lost; transaction discarded (command was not retried): %w", err)
		}
		if command == "EXEC" {
			s.multi = false
		}
		return err
	}
	if result != "QUEUED" {
		switch command {
		case "SELECT":
			if len(args) == 2 {
				s.options.DB, _ = strconv.Atoi(args[1].(string))
			}
		case "AUTH":
			if len(args) == 2 {
				s.options.Username, s.options.Password = "", args[1].(string)
			}
			if len(args) == 3 {
				s.options.Username, s.options.Password = args[1].(string), args[2].(string)
			}
		case "MULTI":
			s.multi = true
		case "EXEC", "DISCARD":
			s.multi = false
		}
	}
	return s.write(w, result)
}

// MONITOR emits a continuous RESP2 stream. A separate connection keeps the
// command session usable after cancellation and avoids driver pool reuse of a
// connection that still has unsolicited replies pending.
func (s *redisSession) openMonitor(ctx context.Context, address string) (net.Conn, *bufio.Reader, error) {
	dialer := &net.Dialer{Timeout: s.options.DialTimeout}
	var conn net.Conn
	var err error
	if s.options.TLSConfig != nil {
		conn, err = (&tls.Dialer{NetDialer: dialer, Config: s.options.TLSConfig.Clone()}).DialContext(ctx, "tcp", address)
	} else {
		conn, err = dialer.DialContext(ctx, "tcp", address)
	}
	if err != nil {
		return nil, nil, err
	}
	success := false
	defer func() {
		if !success {
			_ = conn.Close()
		}
	}()
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()
	reader := bufio.NewReader(conn)
	request := func(args ...string) error {
		if err := conn.SetDeadline(time.Now().Add(10 * time.Second)); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(conn, "*%d\r\n", len(args)); err != nil {
			return err
		}
		for _, arg := range args {
			if _, err := fmt.Fprintf(conn, "$%d\r\n%s\r\n", len(arg), arg); err != nil {
				return err
			}
		}
		line, err := readMonitorLine(reader)
		if err != nil {
			return err
		}
		if line != "+OK" {
			return errors.New(strings.TrimPrefix(line, "-"))
		}
		return nil
	}
	if s.options.Password != "" || s.options.Username != "" && s.options.Username != "default" {
		if s.options.Username != "" {
			err = request("AUTH", s.options.Username, s.options.Password)
		} else {
			err = request("AUTH", s.options.Password)
		}
		if err != nil {
			return nil, nil, err
		}
	}
	if s.options.DB != 0 {
		if err = request("SELECT", strconv.Itoa(s.options.DB)); err != nil {
			return nil, nil, err
		}
	}
	if err = request("MONITOR"); err != nil {
		return nil, nil, err
	}
	if err = conn.SetDeadline(time.Time{}); err != nil {
		return nil, nil, err
	}
	success = true
	return conn, reader, nil
}

func (s *redisSession) monitor(ctx context.Context, w io.Writer) error {
	addresses := []string{s.options.Addr}
	var masters []clusterMaster
	if s.cluster != nil {
		var err error
		masters, err = s.clusterMasters(ctx)
		if err != nil {
			return err
		}
		addresses = addresses[:0]
		for _, master := range masters {
			addresses = append(addresses, master.addr)
		}
	}
	streamCtx, cancel := context.WithCancel(ctx)
	connections := make([]net.Conn, 0, len(addresses))
	var workers sync.WaitGroup
	defer func() {
		cancel()
		for _, conn := range connections {
			_ = conn.Close()
		}
		workers.Wait()
	}()
	type event struct {
		address, line string
		err           error
	}
	events := make(chan event)
	if len(masters) > 0 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			s.watchMonitor(streamCtx, masters, func(err error) {
				select {
				case events <- event{err: err, address: "topology"}:
				case <-streamCtx.Done():
				}
			})
		}()
	}
	for _, address := range addresses {
		conn, reader, err := s.openMonitor(streamCtx, address)
		if err != nil {
			return fmt.Errorf("MONITOR node %s: %w", address, err)
		}
		connections = append(connections, conn)
		workers.Add(1)
		go func() {
			defer workers.Done()
			stop := context.AfterFunc(streamCtx, func() { _ = conn.Close() })
			defer stop()
			for {
				line, err := readMonitorLine(reader)
				select {
				case events <- event{address, line, err}:
				case <-streamCtx.Done():
					return
				}
				if err != nil {
					return
				}
			}
		}()
	}
	if err := s.write(w, "OK"); err != nil {
		return err
	}
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case event := <-events:
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if event.err != nil {
				return fmt.Errorf("MONITOR node %s disconnected: %w", event.address, event.err)
			}
			if !strings.HasPrefix(event.line, "+") {
				return errors.New("unexpected Redis MONITOR response")
			}
			line := strings.TrimPrefix(event.line, "+")
			if s.cluster != nil {
				line = "[" + event.address + "] " + line
			}
			if err := s.write(w, line); err != nil {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				return err
			}
		}
	}
}

func readMonitorLine(reader *bufio.Reader) (string, error) {
	var line []byte
	for {
		part, err := reader.ReadSlice('\n')
		if len(line)+len(part) > 1024*1024 {
			return "", errors.New("Redis MONITOR line exceeds 1 MiB")
		}
		line = append(line, part...)
		if errors.Is(err, bufio.ErrBufferFull) {
			continue
		}
		if err != nil {
			return "", err
		}
		if len(line) < 2 || line[len(line)-2] != '\r' {
			return "", errors.New("invalid Redis MONITOR framing")
		}
		return string(line[:len(line)-2]), nil
	}
}

func (s *redisSession) write(w io.Writer, value any) error {
	if s.json {
		return json.NewEncoder(w).Encode(value)
	}
	switch value := value.(type) {
	case nil:
		_, err := fmt.Fprintln(w, "(nil)")
		return err
	case []any:
		for _, item := range value {
			if err := s.write(w, item); err != nil {
				return err
			}
		}
		return nil
	case []string:
		for _, item := range value {
			if err := s.write(w, item); err != nil {
				return err
			}
		}
		return nil
	case error:
		_, err := fmt.Fprintln(w, "(error)", value)
		return err
	default:
		_, err := fmt.Fprintln(w, value)
		return err
	}
}

func (s *redisSession) subscribe(ctx context.Context, w io.Writer, command string, args []any) error {
	if len(args) == 0 {
		return errors.New("subscription requires at least one channel or pattern")
	}
	channels := make([]string, len(args))
	for i, arg := range args {
		channels[i] = arg.(string)
	}
	// Subscriptions get their own connection; the interactive session's DB,
	// authentication and transaction state remain on its dedicated connection.
	var subscription *redis.PubSub
	if s.cluster != nil {
		switch command {
		case "SUBSCRIBE":
			subscription = s.cluster.Subscribe(ctx, channels...)
		case "PSUBSCRIBE":
			subscription = s.cluster.PSubscribe(ctx, channels...)
		case "SSUBSCRIBE":
			subscription = s.cluster.SSubscribe(ctx, channels...)
		}
	} else {
		opts := *s.options
		client := redis.NewClient(&opts)
		defer client.Close()
		switch command {
		case "SUBSCRIBE":
			subscription = client.Subscribe(ctx, channels...)
		case "PSUBSCRIBE":
			subscription = client.PSubscribe(ctx, channels...)
		case "SSUBSCRIBE":
			subscription = client.SSubscribe(ctx, channels...)
		}
	}
	defer subscription.Close()
	stop := context.AfterFunc(ctx, func() { _ = subscription.Close() })
	defer stop()
	for {
		message, err := subscription.Receive(ctx)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil {
			return err
		}
		switch message := message.(type) {
		case *redis.Subscription:
			if err = s.write(w, []any{message.Kind, message.Channel, message.Count}); err != nil {
				return err
			}
		case *redis.Message:
			values := []any{"message", message.Channel, message.Payload}
			if message.Pattern != "" {
				values = []any{"pmessage", message.Pattern, message.Channel, message.Payload}
			}
			if err = s.write(w, values); err != nil {
				return err
			}
		}
	}
}
