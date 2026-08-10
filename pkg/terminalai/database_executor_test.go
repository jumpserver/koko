package terminalai

import (
	"bufio"
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/jumpserver-dev/sdk-go/model"
	"github.com/jumpserver/koko/pkg/srvconn"
	"github.com/mediocregopher/radix/v3"
	"go.mongodb.org/mongo-driver/bson"
)

func TestDatabaseProtocolsSupportBackground(t *testing.T) {
	protocols := []string{
		srvconn.ProtocolMySQL, srvconn.ProtocolMariadb,
		srvconn.ProtocolPostgresql, srvconn.ProtocolSQLServer,
		srvconn.ProtocolOracle, srvconn.ProtocolClickHouse,
		srvconn.ProtocolRedis, srvconn.ProtocolMongoDB,
	}
	for _, protocol := range protocols {
		if !supportsBackground(SessionContext{Protocol: protocol}) {
			t.Errorf("protocol %s does not support background execution", protocol)
		}
	}
}

func TestDatabaseAdaptersRestrictSessionCommandsToPTY(t *testing.T) {
	redisProposal := CommandProposal{
		Command: "MONITOR", Execution: ExecutionBackground, RiskLevel: 2,
	}
	if err := (&redisAdapter{}).PrepareProposal(&redisProposal); err != nil {
		t.Fatal(err)
	}
	mongoProposal := CommandProposal{
		Command:   `db.runCommand({"commitTransaction":1})`,
		Execution: ExecutionBackground, RiskLevel: 2,
	}
	if err := (&mongoDBAdapter{}).PrepareProposal(&mongoProposal); err != nil {
		t.Fatal(err)
	}
	sqlProposal := CommandProposal{
		Command: "SET search_path TO public", Execution: ExecutionBackground,
		RiskLevel: 2,
	}
	if err := (&sqlAdapter{name: "postgresql"}).PrepareProposal(&sqlProposal); err != nil {
		t.Fatal(err)
	}
	for name, proposal := range map[string]CommandProposal{
		"redis": redisProposal, "mongodb": mongoProposal, "sql": sqlProposal,
	} {
		if proposal.Execution != ExecutionPTY || proposal.BackgroundEligible {
			t.Errorf("%s proposal was not restricted to PTY: %#v", name, proposal)
		}
	}
}

func TestDatabaseAdaptersRequireApprovalForDestructiveCommands(t *testing.T) {
	redisProposal := CommandProposal{Command: "FLUSHALL", RiskLevel: 1}
	if err := (&redisAdapter{}).PrepareProposal(&redisProposal); err != nil {
		t.Fatal(err)
	}
	mongoProposal := CommandProposal{
		Command: `db.runCommand({"dropDatabase":1})`, RiskLevel: 1,
	}
	if err := (&mongoDBAdapter{}).PrepareProposal(&mongoProposal); err != nil {
		t.Fatal(err)
	}
	for name, proposal := range map[string]CommandProposal{
		"redis": redisProposal, "mongodb": mongoProposal,
	} {
		if proposal.RiskLevel != 4 || !proposal.ApprovalRequired {
			t.Errorf("%s destructive command was not protected: %#v", name, proposal)
		}
	}
}

func TestRedisBackgroundCommandValidation(t *testing.T) {
	arguments, err := parseRedisCommand(`SET "user key" 'hello world'`)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"SET", "user key", "hello world"}
	if !reflect.DeepEqual(arguments, want) {
		t.Fatalf("arguments = %#v, want %#v", arguments, want)
	}
	for _, command := range []string{"MONITOR", "XREAD BLOCK 0 STREAMS events $"} {
		arguments, err = parseRedisCommand(command)
		if err != nil {
			t.Fatal(err)
		}
		if redisBackgroundEligible(arguments) {
			t.Errorf("command %q unexpectedly supports background execution", command)
		}
	}
}

func TestRedisRESPOutput(t *testing.T) {
	response := &redisRESPOutput{output: &boundedDatabaseOutput{}}
	err := response.UnmarshalRESP(bufio.NewReader(strings.NewReader(
		"*4\r\n$3\r\nfoo\r\n:42\r\n$-1\r\n*0\r\n",
	)))
	if err != nil {
		t.Fatal(err)
	}
	if value := response.output.String(); value != "foo\n42\n(nil)\n(empty array)" {
		t.Fatalf("unexpected Redis response output %q", value)
	}

	errorResponse := &redisRESPOutput{output: &boundedDatabaseOutput{}}
	err = errorResponse.UnmarshalRESP(bufio.NewReader(strings.NewReader("-ERR denied\r\n")))
	if err == nil || err.Error() != "ERR denied" || errorResponse.output.String() != "" {
		t.Fatalf("unexpected Redis error response: output=%q err=%v", errorResponse.output.String(), err)
	}
}

func TestRedisExecutorUsesDriver(t *testing.T) {
	connection := radix.Stub("tcp", "redis", func(command []string) any {
		if !reflect.DeepEqual(command, []string{"MGET", "first", "second"}) {
			t.Fatalf("unexpected Redis command %#v", command)
		}
		return []string{"one", "two"}
	})
	executor := &RedisExecutor{client: connection}
	defer executor.Close()
	output, _, err := executor.Execute(
		context.Background(), "MGET first second", nil,
	)
	if err != nil || output != "one\ntwo" {
		t.Fatalf("unexpected Redis driver result: output=%q err=%v", output, err)
	}
}

func TestParseMongoDBBackgroundCommand(t *testing.T) {
	document, err := parseMongoDBCommand(
		`db.runCommand({"find":"users","filter":{"active":true}});`,
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(document) != 2 || document[0].Key != "find" {
		t.Fatalf("unexpected command document: %#v", document)
	}
	if _, err = parseMongoDBCommand(`db.users.find({})`); err == nil {
		t.Fatal("MongoDB shell helper unexpectedly accepted for background execution")
	}
	getMore, err := parseMongoDBCommand(`db.runCommand({"getMore":1,"collection":"users"})`)
	if err != nil {
		t.Fatal(err)
	}
	if mongoDBBackgroundEligible(getMore) {
		t.Fatal("MongoDB cursor continuation unexpectedly supports background execution")
	}
}

func TestNativeSQLDatabaseDrivers(t *testing.T) {
	for protocol, port := range map[string]int{
		srvconn.ProtocolPostgresql: 5432,
		srvconn.ProtocolSQLServer:  1433,
		srvconn.ProtocolOracle:     1521,
		srvconn.ProtocolClickHouse: 9000,
	} {
		db, err := newNativeSQLDatabase(DatabaseConfig{
			Protocol: protocol, Host: "127.0.0.1", Port: port,
			Username: "user", Password: "secret", Database: "app",
		})
		if err != nil {
			t.Fatalf("configure %s native driver: %v", protocol, err)
		}
		if err = db.Close(); err != nil {
			t.Fatalf("close %s native driver: %v", protocol, err)
		}
	}
}

func TestMongoDBBackgroundOutputMasking(t *testing.T) {
	sanitizer := NewMySQLSanitizer([]model.DataMaskingRule{{
		FieldsPattern: "password", MaskingMethod: "fixed_char",
		MaskPattern: "******", IsActive: true,
	}})
	document := sanitizeMongoDBDocument(
		bson.D{{Key: "password", Value: "cleartext"}}, sanitizer,
	)
	if document[0].Value != "******" {
		t.Fatalf("MongoDB value was not masked: %#v", document)
	}
}

func TestBoundedDatabaseOutput(t *testing.T) {
	output := &boundedDatabaseOutput{}
	_, _ = output.Write([]byte(strings.Repeat("x", maxDatabaseOutput+1)))
	value := output.String()
	if len(value) > maxDatabaseOutput || !strings.Contains(value, "output truncated") {
		t.Fatalf("unexpected bounded output length %d", len(value))
	}
}
