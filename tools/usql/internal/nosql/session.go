// Package nosql provides native MongoDB and Redis sessions in the usql binary.
package nosql

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/gohxs/readline"
	"github.com/xo/usql/rline"
)

type Input struct {
	Command bool
	Value   string
}

type Config struct {
	DSN           string
	ForcePassword bool
	NoPassword    bool
	JSON          bool
	Quiet         bool
	StopOnError   bool
	Inputs        []Input
	StartupFile   string
	Init          string
}

type session interface {
	Execute(context.Context, io.Writer, string) error
	Prompt() string
	Close() error
}

var errQuit = errors.New("quit")

func IsDSN(dsn string) bool {
	scheme, _, _ := strings.Cut(dsn, ":")
	switch strings.ToLower(scheme) {
	case "mongodb", "mongodb+srv", "redis", "rediss":
		return true
	}
	return false
}

// Run shares usql's terminal, history, password input and command/file entry points.
// Document and RESP commands deliberately bypass the SQL statement parser.
func Run(ctx context.Context, terminal rline.IO, config Config) error {
	u, err := url.Parse(config.DSN)
	if err != nil {
		return errors.New("invalid NoSQL connection URL")
	}
	u.Scheme = strings.ToLower(u.Scheme)
	if u.RawQuery != "" {
		q, err := url.ParseQuery(u.RawQuery)
		if err != nil {
			return errors.New("invalid NoSQL connection options")
		}
		if q.Has("data-masking-rules") {
			return errors.New("data masking is not supported by NoSQL sessions")
		}
	}
	passwordMissing := false
	if u.User != nil {
		_, supplied := u.User.Password()
		passwordMissing = !supplied && u.User.Username() != ""
	}
	if config.ForcePassword || (passwordMissing && !config.NoPassword && terminal.Interactive()) {
		password, err := terminal.Password("Enter password: ")
		if err != nil {
			return err
		}
		username := ""
		if u.User != nil {
			username = u.User.Username()
		}
		u.User = url.UserPassword(username, password)
	}
	var backend session
	connectCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	if strings.HasPrefix(u.Scheme, "mongodb") {
		backend, err = openMongo(connectCtx, u, config.JSON)
	} else {
		backend, err = openRedis(connectCtx, u, config.JSON)
	}
	cancel()
	if err != nil {
		return err
	}
	defer backend.Close()
	r := &runner{ctx: ctx, terminal: terminal, session: backend, config: config, mongo: strings.HasPrefix(u.Scheme, "mongodb")}
	if terminal.Interactive() {
		words := []string{"help", "exit", "quit"}
		if r.mongo {
			words = append(words, "show dbs", "show collections", "use", "db.runCommand", "db.getCollection", "db.stats()", "db.version()")
		} else {
			words = append(words, "PING", "GET", "SET", "DEL", "SCAN", "HGETALL", "INFO", "SELECT", "MULTI", "EXEC", "DISCARD", "SUBSCRIBE", "PSUBSCRIBE", "MONITOR", `\cluster info`)
		}
		items := make([]readline.PrefixCompleterInterface, 0, len(words))
		for _, word := range words {
			items = append(items, readline.PcItem(word))
		}
		terminal.Completer(readline.NewPrefixCompleter(items...))
		if !config.Quiet {
			fmt.Fprintln(terminal.Stdout(), "usql native "+u.Scheme+" session. Type help for supported commands.")
		}
	}
	if config.StartupFile != "" {
		if err = r.file(config.StartupFile); err != nil && !errors.Is(err, os.ErrNotExist) {
			return r.finish(err)
		}
	}
	if config.Init != "" {
		if err = r.script(config.Init); err != nil {
			return r.finish(err)
		}
	}
	if len(config.Inputs) > 0 {
		for _, input := range config.Inputs {
			if input.Command {
				err = r.script(input.Value)
			} else {
				err = r.file(input.Value)
			}
			if err != nil {
				return r.finish(err)
			}
		}
		return r.lastErr
	}
	var pending string
	for {
		if err = ctx.Err(); err != nil {
			return err
		}
		prompt := backend.Prompt()
		if pending != "" {
			prompt = "... "
		}
		terminal.Prompt(prompt)
		line, readErr := terminal.Next()
		if errors.Is(readErr, rline.ErrInterrupt) {
			pending = ""
			continue
		}
		if len(line) > 0 {
			pending += string(line) + "\n"
		}
		if len(pending) > 1024*1024 {
			return errors.New("command exceeds 1 MiB")
		}
		if readErr != nil && !errors.Is(readErr, io.EOF) {
			return readErr
		}
		statements, incomplete, parseErr := r.statements(pending)
		if incomplete && !errors.Is(readErr, io.EOF) {
			continue
		}
		if incomplete {
			parseErr = errors.New("incomplete command at end of input")
		}
		if parseErr == nil {
			for _, statement := range statements {
				r.saveHistory(statement)
				if err = r.execute(statement); err != nil {
					if errors.Is(err, errQuit) {
						return nil
					}
					if config.StopOnError && !terminal.Interactive() {
						return err
					}
				}
			}
		} else {
			r.lastErr = parseErr
			fmt.Fprintln(terminal.Stderr(), "error:", parseErr)
		}
		pending = ""
		if errors.Is(readErr, io.EOF) {
			return r.lastErr
		}
	}
}

type runner struct {
	ctx      context.Context
	terminal rline.IO
	session  session
	config   Config
	mongo    bool
	lastErr  error
}

func (r *runner) saveHistory(statement string) {
	if !r.terminal.Interactive() || sensitive(statement, r.mongo) {
		return
	}
	// Readline edits physical lines, not whole JavaScript programs. In particular,
	// the newline appended by the input loop must never be recalled into its buffer.
	for _, line := range strings.Split(statement, "\n") {
		line = strings.TrimSuffix(line, "\r")
		if strings.TrimSpace(line) != "" {
			_ = r.terminal.Save(line)
		}
	}
}

func (r *runner) finish(err error) error {
	if errors.Is(err, errQuit) {
		return nil
	}
	return err
}

func (r *runner) execute(statement string) error {
	control := strings.TrimSpace(statement)
	if !r.mongo {
		control = strings.ToLower(control)
	}
	switch control {
	case "exit", "quit", "exit()", "quit()", `\q`:
		return errQuit
	case "help", "help()", `\?`:
		if r.mongo {
			_, err := fmt.Fprintln(r.terminal.Stdout(), "MongoDB: use <database>; show dbs; show collections; db.<collection>.<method>(...); db.runCommand({...}).\nJavaScript, BSON/EJSON, load(), cursors (it/toArray/forEach), bulk writes, sessions/transactions, change streams, users/roles and rs/sh helpers are available. Node.js modules and client-side field encryption are not included. See README for compatibility limits.")
			return err
		}
		_, err := fmt.Fprintln(r.terminal.Stdout(), "Redis: enter Redis commands with quoted arguments and \\n/\\r/\\t/\\xHH escapes. SELECT, MULTI/EXEC and WATCH use a persistent connection; pub/sub and MONITOR stream until Ctrl-C. URL option cluster=true enables Cluster routing, global DBSIZE/KEYS/SCAN and INFO keyspace; \\cluster info shows per-master statistics. Stateful Cluster commands require a direct node connection.")
		return err
	}
	ctx, stop := signal.NotifyContext(r.ctx, os.Interrupt)
	err := r.session.Execute(ctx, r.terminal.Stdout(), statement)
	stop()
	if err != nil {
		r.lastErr = err
		fmt.Fprintln(r.terminal.Stderr(), "error:", err)
	}
	return err
}

func (r *runner) script(script string) error {
	statements, incomplete, err := r.statements(script)
	if err != nil {
		return err
	}
	if incomplete {
		return errors.New("incomplete command at end of input")
	}
	for _, statement := range statements {
		if err = r.execute(statement); err != nil {
			// Explicit -c/-f input must report failures to its caller.
			return err
		}
	}
	return nil
}

func (r *runner) file(name string) error {
	if backend, ok := r.session.(interface {
		ExecuteFile(context.Context, io.Writer, string) error
	}); ok {
		ctx, stop := signal.NotifyContext(r.ctx, os.Interrupt)
		defer stop()
		return backend.ExecuteFile(ctx, r.terminal.Stdout(), name)
	}
	file, err := os.Open(name)
	if err != nil {
		return err
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 4096), 1024*1024)
	var pending string
	for scanner.Scan() {
		pending += scanner.Text() + "\n"
		_, incomplete, err := r.statements(pending)
		if err != nil {
			return err
		}
		if !incomplete {
			if err = r.script(pending); err != nil {
				return err
			}
			pending = ""
		}
		if len(pending) > 1024*1024 {
			return errors.New("command exceeds 1 MiB")
		}
	}
	if err = scanner.Err(); err != nil {
		return err
	}
	return r.script(pending)
}

func (r *runner) statements(source string) ([]string, bool, error) {
	if backend, ok := r.session.(interface {
		Statements(string) ([]string, bool, error)
	}); ok {
		return backend.Statements(source)
	}
	return splitStatements(source, r.mongo)
}

func sensitive(statement string, mongo bool) bool {
	upper := strings.ToUpper(strings.TrimSpace(statement))
	if mongo {
		return strings.Contains(upper, "PASSWORD") || strings.Contains(upper, "PWD") || strings.Contains(upper, "AUTH") || strings.Contains(upper, "MONGODB://") || strings.Contains(upper, "MONGODB+SRV://") || strings.Contains(upper, "CONNECT(") || strings.Contains(upper, "MONGO(")
	}
	args, err := redisArgs(statement)
	if err != nil || len(args) == 0 {
		return true
	}
	command := strings.ToUpper(args[0].(string))
	return command == "AUTH" || command == "HELLO" || command == "ACL" && len(args) > 1 && strings.EqualFold(args[1].(string), "SETUSER")
}
