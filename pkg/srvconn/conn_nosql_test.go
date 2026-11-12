package srvconn

import (
	"bufio"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestNativeNoSQLArgs(t *testing.T) {
	opt := &sqlOption{Host: "2001:db8::1", Port: 27017, DBName: "audit", Username: "user@tenant", Password: "never-in-argv",
		AuthSource: "admin", UseSSL: true, CaCertPath: "/tmp/ca.pem", CertKeyPath: "/tmp/client.pem", ProxyURL: "http://127.0.0.1:18080"}
	args := opt.MongoDBCommandArgs()
	u, err := url.Parse(args[0])
	if err != nil {
		t.Fatal(err)
	}
	if u.Scheme != "mongodb" || u.Hostname() != opt.Host || u.Path != "/audit" || u.User.Username() != opt.Username ||
		u.Query().Get("authSource") != "admin" || u.Query().Get("proxy") != opt.ProxyURL || u.Query().Get("tls") != "true" {
		t.Fatal("MongoDB endpoint, authentication, TLS or proxy changed")
	}
	if _, present := u.User.Password(); present {
		t.Fatal("password must be supplied through the PTY")
	}
	if strings.Contains(strings.Join(args, " "), opt.Password) || !slices.Contains(args, "--password") {
		t.Fatal("password leaked in argv or login prompt was not requested")
	}
	opt.Password = ""
	args = opt.MongoDBCommandArgs()
	u, _ = url.Parse(args[0])
	if password, present := u.User.Password(); !present || password != "" || !slices.Contains(args, "--no-password") {
		t.Fatal("empty password should not open a separate prompt")
	}
}

func TestLiveNativeNoSQLPTY(t *testing.T) {
	binary := os.Getenv("USQL_NOSQL_TEST_BINARY")
	if binary == "" {
		t.Skip("set USQL_NOSQL_TEST_BINARY and test-only MongoDB/Redis DSN files")
	}
	dir := t.TempDir()
	if err := os.Symlink(binary, filepath.Join(dir, "usql")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	previous := debug
	debug = "true"
	t.Cleanup(func() { debug = previous })
	for _, test := range []struct{ name, variable, command, want, echo, suffix string }{
		{"MongoDB", "USQL_MONGODB_TEST_DSN_FILE", `db.runCommand(EJSON.deserialize({"ping":1},{"relaxed":false}))`, `"ok"`, "print('", "')"},
		{"Redis", "USQL_REDIS_TEST_DSN_FILE", "PING", "PONG", "ECHO '", "'"},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := os.Getenv(test.variable)
			if path == "" {
				t.Skip("missing test DSN file")
			}
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			u, err := url.Parse(strings.TrimSpace(string(data)))
			if err != nil {
				t.Fatal("invalid test DSN")
			}
			port, _ := strconv.Atoi(u.Port())
			password, _ := u.User.Password()
			opts := []SqlOption{SqlHost(u.Hostname()), SqlPort(port), SqlUsername(u.User.Username()), SqlPassword(password), SqlDBName(strings.TrimPrefix(u.Path, "/")), SqlAuthSource(u.Query().Get("authSource")), SqlPtyWin(Windows{Width: 120, Height: 24})}
			var conn ServerConnection
			if test.name == "MongoDB" {
				conn, err = NewMongoDBConnection(opts...)
			} else {
				conn, err = NewRedisConnection(opts...)
			}
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			reader := bufio.NewReader(conn)
			readUntil := func(want string) string {
				t.Helper()
				var output strings.Builder
				done := make(chan error, 1)
				go func() {
					for output.Len() < 64*1024 {
						b, err := reader.ReadByte()
						if err != nil {
							done <- err
							return
						}
						output.WriteByte(b)
						if strings.Contains(output.String(), "Enter password:") {
							done <- io.ErrNoProgress
							return
						}
						if password != "" && strings.Contains(output.String(), password) {
							done <- io.ErrUnexpectedEOF
							return
						}
						if strings.Contains(output.String(), want) {
							done <- nil
							return
						}
					}
					done <- io.ErrShortBuffer
				}()
				select {
				case err := <-done:
					if err != nil {
						t.Fatalf("PTY output/credential check failed: %v", err)
					}
				case <-time.After(8 * time.Second):
					_ = conn.Close()
					t.Fatal("PTY prompt/output timed out")
				}
				return output.String()
			}
			write := func(input string) {
				t.Helper()
				if _, err := conn.Write([]byte(input)); err != nil {
					t.Fatal(err)
				}
			}
			// The native password exchange must preserve the complete first prompt.
			prompt := "mongodb:" + strings.TrimPrefix(u.Path, "/") + "> "
			if test.name == "Redis" {
				prompt = "redis:" + u.Host + "[" + strings.TrimPrefix(u.Path, "/") + "]> "
			}
			if output := readUntil("> "); !strings.Contains(output, prompt) {
				t.Fatal("initial shell prompt was truncated")
			}
			write(test.command + "\r")
			readUntil(test.want)
			readUntil("> ")
			for _, marker := range []string{"tapd-first", "tapd-second"} {
				write(test.echo + marker + test.suffix + "\r")
				readUntil("\r\n" + marker + "\r\n")
				readUntil("> ")
			}
			// Up recalls history; left/right move within it rather than changing
			// entries. Backspace and insertion must edit the recalled command.
			write("\x1b[A" + strings.Repeat("\x1b[D", len(test.suffix)+1) + "\x1b[C\x7fD")
			if output := readUntil(test.echo + "tapd-seconD" + test.suffix); strings.Contains(output, "\n") {
				t.Fatal("history repaint introduced a newline")
			}
			write("\r")
			readUntil("\r\ntapd-seconD\r\n")
			readUntil("> ")
			write("\x1b[A\x1b[A\x1b[B\r")
			readUntil("\r\ntapd-seconD\r\n")
			readUntil("> ")
		})
	}
}

func TestNativeRedisArgs(t *testing.T) {
	opt := &sqlOption{Host: "2001:db8::2", Port: 6380, DBName: "0", Username: "default", Password: "never-in-argv",
		ClusterMode: true, UseSSL: true, CaCertPath: "/tmp/ca.pem", ClientCertPath: "/tmp/client.pem", CertKeyPath: "/tmp/client.key", ServerName: "redis.example"}
	args := opt.RedisCommandArgs()
	u, err := url.Parse(args[0])
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	if u.Scheme != "rediss" || u.Hostname() != opt.Host || u.Path != "/0" || q.Get("cluster") != "true" ||
		q.Get("tls-ca") != opt.CaCertPath || q.Get("tls-cert") != opt.ClientCertPath || q.Get("tls-key") != opt.CertKeyPath || q.Get("tls-server-name") != opt.ServerName {
		t.Fatal("Redis endpoint, Cluster or TLS configuration changed")
	}
	if strings.Contains(strings.Join(args, " "), opt.Password) || !slices.Contains(args, "--password") || !slices.Contains(args, "--no-init") {
		t.Fatal("Redis must use protected PTY login and suppress local initialization")
	}
}
