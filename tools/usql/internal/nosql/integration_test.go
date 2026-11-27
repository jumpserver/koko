package nosql

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

func liveURL(t *testing.T, variable string) *url.URL {
	t.Helper()
	path := os.Getenv(variable)
	if path == "" {
		t.Skip("set " + variable + " to a private DSN file")
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(strings.TrimSpace(string(contents)))
	if err != nil {
		t.Fatal("invalid test DSN")
	}
	return u
}

func TestLiveMongoDB(t *testing.T) {
	u := liveURL(t, "USQL_MONGODB_TEST_DSN_FILE")
	database := fmt.Sprintf("usql_nosql_%d", time.Now().UnixNano())
	u.Path = "/" + database
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	backend, err := openMongo(ctx, u, false)
	if err != nil {
		t.Fatal(err)
	}
	s := backend.(*mongoSession)
	t.Cleanup(func() { _ = s.client.Database(database).Drop(context.Background()); _ = s.Close() })
	for _, command := range []string{
		`db.records.insertMany([{name:"中文🙂", n:NumberLong("9007199254740993")},{name:"second",n:NumberLong("2")}])`,
		`db.records.createIndex({name:1})`,
		`db.records.updateOne({name:"second"},{$set:{updated:true}})`,
		`db.records.aggregate([{$match:{updated:true}},{$project:{_id:0,name:1}}])`,
		`db.runCommand({ping:1})`,
		`show collections`,
	} {
		var output bytes.Buffer
		if err := s.Execute(ctx, &output, command); err != nil {
			t.Fatalf("%s: %v", command, err)
		}
	}
	var output bytes.Buffer
	if err := s.Execute(ctx, &output, `db.records.find({name:/中文/},{_id:0}).sort({name:1}).limit(1)`); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "中文🙂") || !strings.Contains(output.String(), `"9007199254740993"`) {
		t.Fatalf("BSON/Unicode output: %s", output.String())
	}
	output.Reset()
	if err := s.Execute(ctx, &output, `db.records.countDocuments({})`); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), `"2"`) {
		t.Fatalf("count=%s", output.String())
	}
	if err := s.Execute(ctx, io.Discard, "use admin"); err != nil || s.database != "admin" {
		t.Fatal("database switch failed", err)
	}
	if err := s.Execute(ctx, io.Discard, `let x = 1; assert.eq(2,x+1)`); err != nil {
		t.Fatal("JavaScript execution failed", err)
	}
}

func TestLiveMongoDBHTTPProxy(t *testing.T) {
	u := liveURL(t, "USQL_MONGODB_TEST_DSN_FILE")
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodConnect || r.Host != u.Host {
			http.Error(w, "unexpected CONNECT target", http.StatusForbidden)
			return
		}
		upstream, err := net.DialTimeout("tcp", u.Host, time.Second)
		if err != nil {
			http.Error(w, "dial failed", http.StatusBadGateway)
			return
		}
		defer upstream.Close()
		downstream, _, err := w.(http.Hijacker).Hijack()
		if err != nil {
			return
		}
		defer downstream.Close()
		_, _ = io.WriteString(downstream, "HTTP/1.1 200 Connection Established\r\n\r\n")
		done := make(chan struct{})
		go func() { _, _ = io.Copy(upstream, downstream); _ = upstream.Close(); close(done) }()
		_, _ = io.Copy(downstream, upstream)
		_ = downstream.Close()
		<-done
	}))
	defer proxy.Close()
	q := u.Query()
	q.Set("proxy", proxy.URL)
	u.RawQuery = q.Encode()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	backend, err := openMongo(ctx, u, false)
	if err != nil {
		t.Fatal(err)
	}
	defer backend.Close()
	if err := backend.Execute(ctx, io.Discard, `db.runCommand({ping:1})`); err != nil {
		t.Fatal(err)
	}
}

func TestLiveNoSQLTLS(t *testing.T) {
	for _, test := range []struct {
		name, variable string
		open           func(context.Context, *url.URL, bool) (session, error)
	}{
		{"MongoDB", "USQL_MONGODB_TLS_TEST_DSN_FILE", openMongo},
		{"Redis", "USQL_REDIS_TLS_TEST_DSN_FILE", openRedis},
	} {
		t.Run(test.name, func(t *testing.T) {
			u := liveURL(t, test.variable)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			backend, err := test.open(ctx, u, false)
			if err != nil {
				t.Fatal(err)
			}
			defer backend.Close()
			command := "PING"
			if test.name == "MongoDB" {
				command = `db.runCommand({ping:1})`
			}
			if err := backend.Execute(ctx, io.Discard, command); err != nil {
				t.Fatal(err)
			}
			q := u.Query()
			q.Del("tls-ca")
			q.Del("tlsCAFile")
			u.RawQuery = q.Encode()
			badCtx, stop := context.WithTimeout(context.Background(), 250*time.Millisecond)
			defer stop()
			if unexpected, err := test.open(badCtx, u, false); err == nil {
				_ = unexpected.Close()
				t.Fatal("untrusted TLS certificate was accepted")
			}
		})
	}
}

func TestLiveRedis(t *testing.T) {
	u := liveURL(t, "USQL_REDIS_TEST_DSN_FILE")
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	backend, err := openRedis(ctx, u, false)
	if err != nil {
		t.Fatal(err)
	}
	s := backend.(*redisSession)
	defer s.Close()
	key := fmt.Sprintf("usql:nosql:%d", time.Now().UnixNano())
	defer func() {
		_ = s.Execute(context.Background(), io.Discard, "SELECT 0")
		_ = s.Execute(context.Background(), io.Discard, "DEL "+key)
		_ = s.Execute(context.Background(), io.Discard, "SELECT 1")
		_ = s.Execute(context.Background(), io.Discard, "DEL "+key)
	}()
	for _, command := range []string{"SELECT 1", fmt.Sprintf(`SET %s "中文🙂\x00tail"`, key), "MULTI", "INCR " + key + ":counter", "DEL " + key + ":counter", "EXEC", "WATCH " + key, "UNWATCH"} {
		if err := s.Execute(ctx, io.Discard, command); err != nil {
			t.Fatalf("%s: %v", strings.Fields(command)[0], err)
		}
	}
	var output bytes.Buffer
	if err := s.Execute(ctx, &output, "GET "+key); err != nil {
		t.Fatal(err)
	}
	if output.String() != "中文🙂\x00tail\n" {
		t.Fatalf("binary roundtrip=%q", output.String())
	}
	if s.options.DB != 1 {
		t.Fatal("SELECT was not preserved")
	}
	// Cancel a genuinely blocked command and verify that the next command
	// reconnects to the selected DB without retrying the blocked operation.
	blocked, stop := context.WithTimeout(ctx, 100*time.Millisecond)
	defer stop()
	if err := s.Execute(blocked, io.Discard, "BLPOP "+key+":empty 0"); err == nil {
		t.Fatal("blocked command was not canceled")
	}
	output.Reset()
	if err := s.Execute(ctx, &output, "GET "+key); err != nil || output.String() != "中文🙂\x00tail\n" {
		t.Fatalf("session after cancel: %q %v", output.String(), err)
	}
}

func TestLiveRedisStreams(t *testing.T) {
	u := liveURL(t, "USQL_REDIS_TEST_DSN_FILE")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	opts, _, err := redisOptions(u)
	if err != nil {
		t.Fatal(err)
	}
	publisher := redis.NewClient(opts)
	defer publisher.Close()
	for _, command := range []string{"SUBSCRIBE usql_nosql_channel", "MONITOR"} {
		t.Run(command, func(t *testing.T) {
			backend, err := openRedis(ctx, u, false)
			if err != nil {
				t.Fatal(err)
			}
			defer backend.Close()
			streamCtx, stop := context.WithCancel(ctx)
			defer stop()
			reader, writer := io.Pipe()
			defer reader.Close()
			done := make(chan error, 1)
			go func() { err := backend.Execute(streamCtx, writer, command); _ = writer.Close(); done <- err }()
			scanner := bufio.NewScanner(reader)
			if !scanner.Scan() {
				t.Fatal("stream never acknowledged subscription/monitor")
			}
			if err := publisher.Publish(ctx, "usql_nosql_channel", "usql_stream_probe").Err(); err != nil {
				t.Fatal(err)
			}
			found := false
			for scanner.Scan() {
				if strings.Contains(scanner.Text(), "usql_stream_probe") {
					found = true
					break
				}
			}
			if !found {
				t.Fatal("stream did not deliver published message")
			}
			stop()
			if err := <-done; !errors.Is(err, context.Canceled) {
				t.Fatalf("stream cancellation: %v", err)
			}
			if err := backend.Execute(ctx, io.Discard, "PING"); err != nil {
				t.Fatal(err)
			}
		})
	}
}
