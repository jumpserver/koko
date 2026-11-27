package nosql

import (
	"bytes"
	"context"
	"errors"
	"github.com/redis/go-redis/v9"
	"io"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"github.com/xo/usql/rline"
	"go.mongodb.org/mongo-driver/v2/bson"
)

func TestCanonicalBSONOutput(t *testing.T) {
	var output bytes.Buffer
	err := writeExtendedJSON(&output, bson.D{{Key: "long", Value: int64(9007199254740993)}, {Key: "id", Value: bson.ObjectID{1}}}, false)
	if err != nil || !strings.Contains(output.String(), `"$numberLong":"9007199254740993"`) || !strings.Contains(output.String(), `"$oid"`) {
		t.Fatalf("output=%s err=%v", output.String(), err)
	}
}

func TestClusterTransportFailureCannotTriggerDriverRetry(t *testing.T) {
	if err := withoutRedisRetry(io.EOF); errors.Is(err, io.EOF) || err == nil {
		t.Fatal("ClusterClient could replay a write after EOF")
	}
	for _, original := range []error{redis.Nil, context.Canceled, redisServerError("MOVED 1 127.0.0.1:6379"), redisServerError("ASK 1 127.0.0.1:6379")} {
		if err := withoutRedisRetry(original); err != original {
			t.Fatal("redirect/nil/cancellation contract changed")
		}
	}
}

type redisServerError string

func (e redisServerError) Error() string { return string(e) }
func (redisServerError) RedisError()     {}

func TestRedisOptions(t *testing.T) {
	for _, raw := range []string{
		"redis://localhost/1?cluster=true", "redis://localhost/-1", "redis://localhost/0?tls-ca=x",
		"rediss://localhost/0?tls-cert=x", "redis://localhost/0?cluster=maybe", "redis://localhost/0?cluster=true&cluster=false",
		"redis://localhost/0?unknown=1",
	} {
		u, _ := url.Parse(raw)
		if _, _, err := redisOptions(u); err == nil {
			t.Errorf("accepted invalid options %s", raw)
		}
	}
	u, _ := url.Parse("rediss://user:p%40ss@[::1]:6380/2?tls-server-name=redis.example")
	options, clustered, err := redisOptions(u)
	if err != nil {
		t.Fatal(err)
	}
	if clustered || options.Username != "user" || options.Password != "p@ss" || options.DB != 2 || options.TLSConfig.ServerName != "redis.example" || options.TLSConfig.InsecureSkipVerify {
		t.Fatal("credentials, DB, or TLS configuration changed")
	}
}

func TestScriptRejectsIncompleteInputBeforeExecution(t *testing.T) {
	backend := &recordingSession{}
	r := &runner{ctx: context.Background(), session: backend, mongo: true, terminal: &rline.Rline{Out: io.Discard, Err: io.Discard}}
	if err := r.script("db.c.insertOne({x:1}); db.c.find({"); err == nil {
		t.Fatal("accepted incomplete script")
	}
	if len(backend.commands) != 0 {
		t.Fatal("partially executed malformed -c input")
	}
	if err := r.script("use test; db.c.find({})"); err != nil {
		t.Fatal(err)
	}
	if len(backend.commands) != 2 {
		t.Fatalf("executed %v", backend.commands)
	}
	r.mongo = false
	if err := r.script("QUIT"); !errors.Is(err, errQuit) || len(backend.commands) != 2 {
		t.Fatal("Redis QUIT was sent to the server instead of closing the session")
	}
}

func TestAuthenticationHistoryAndPasswordPolicy(t *testing.T) {
	for _, command := range []string{"AUTH secret", "ACL SETUSER test >secret", "HELLO 3 AUTH user secret", `"AUTH" "secret"`, `'ACL' "SETUSER" test >secret`} {
		if !sensitive(command, false) {
			t.Errorf("authentication command would enter history: %s", strings.Fields(command)[0])
		}
	}
	terminal := &rline.Rline{Out: io.Discard, Err: io.Discard, Pw: func(string) (string, error) { return "", errors.New("password requested") }}
	if err := Run(context.Background(), terminal, Config{DSN: "redis://localhost/0", ForcePassword: true}); err == nil || err.Error() != "password requested" {
		t.Fatalf("forced password err=%v", err)
	}
}

func TestHistoryStoresEditableLinesWithoutCredentials(t *testing.T) {
	for _, test := range []struct {
		name, command string
		mongo         bool
		want          []string
	}{
		{"MongoDB", "db.version()\n", true, []string{"db.version()"}},
		{"multiline", "if (true) {\r\n  print('ok');\r\n}\r\n", true, []string{"if (true) {", "  print('ok');", "}"}},
		{"blank", "\n \r\n", true, nil},
		{"Redis", "PING", false, []string{"PING"}},
		{"MongoDB credentials", "db.auth({\nuser:'test',\npwd:'secret'\n})\n", true, nil},
		{"Redis credentials", "AUTH secret", false, nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			var saved []string
			terminal := &rline.Rline{Int: true, S: func(line string) error {
				saved = append(saved, line)
				return nil
			}}
			r := &runner{terminal: terminal, mongo: test.mongo}
			r.saveHistory(test.command)
			if !reflect.DeepEqual(saved, test.want) {
				t.Fatalf("history=%q, want %q", saved, test.want)
			}
		})
	}
}

type recordingSession struct{ commands []string }

func (s *recordingSession) Execute(_ context.Context, _ io.Writer, command string) error {
	s.commands = append(s.commands, command)
	return nil
}
func (*recordingSession) Prompt() string { return "> " }
func (*recordingSession) Close() error   { return nil }
