package assetexec

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jumpserver-dev/sdk-go/model"
)

// The opt-in fixture points only at isolated local test databases; credentials
// stay in its file and are never included in test output.
func TestNativeDatabaseTransactionsAndCancellation(t *testing.T) {
	fixture := os.Getenv("KOKO_AGENT_DATABASE_TEST_CONFIG")
	if fixture == "" {
		t.Skip("isolated database fixture not configured")
	}
	var config struct {
		MySQLPort    int    `json:"mysql_port"`
		PostgresPort int    `json:"postgres_port"`
		Password     string `json:"password"`
		Database     string `json:"database"`
	}
	data, err := os.ReadFile(fixture)
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(data, &config); err != nil {
		t.Fatal(err)
	}
	for _, database := range []struct {
		protocol, username, prepare, deallocate, sleep string
		port                                           int
	}{
		{"mysql", "root", "PREPARE agent_stmt FROM 'SELECT 41 AS n'", "DEALLOCATE PREPARE agent_stmt", "SELECT SLEEP(5)", config.MySQLPort},
		{"postgresql", "agent_test", "PREPARE agent_stmt AS SELECT 41 AS n", "DEALLOCATE agent_stmt", "SELECT pg_sleep(5)", config.PostgresPort},
	} {
		t.Run(database.protocol, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			token := &model.ConnectToken{Id: "isolated-test", User: model.User{ID: "test-user"}, Protocol: database.protocol,
				Actions: model.Actions{{Value: model.ActionConnect}},
				Account: model.Account{BaseAccount: model.BaseAccount{Username: database.username, Secret: config.Password}},
				Asset:   model.Asset{ID: "test-asset", Address: "127.0.0.1", Protocols: []model.Protocol{{Name: database.protocol, Port: database.port}}, SpecInfo: model.SpecInfo{DBName: config.Database}}}
			conn, err := Open(ctx, token, Options{ConnectTimeout: 3 * time.Second, MaxRows: 10, MaxOutputBytes: 4096})
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			execute := func(command string) string {
				t.Helper()
				var output strings.Builder
				result, err := conn.Execute(ctx, Request{Command: command}, func(_ string, p []byte) error { _, _ = output.Write(p); return nil })
				if err != nil || !result.Completed || result.ContextLost {
					t.Fatalf("%s failed: %+v %v", command, result, err)
				}
				return output.String()
			}
			if output := execute("SELECT 1 AS n"); output != "n\n1\n" {
				t.Fatalf("unexpected query output: %q", output)
			}
			execute("CREATE TEMPORARY TABLE assetexec_txn (id INTEGER)")
			execute("BEGIN")
			execute("INSERT INTO assetexec_txn VALUES (1)")
			execute("ROLLBACK")
			if output := execute("SELECT COUNT(*) AS n FROM assetexec_txn"); output != "n\n0\n" {
				t.Fatalf("rollback failed: %q", output)
			}
			execute("BEGIN")
			execute("INSERT INTO assetexec_txn VALUES (2)")
			execute("COMMIT")
			if output := execute("SELECT id AS n FROM assetexec_txn"); output != "n\n2\n" {
				t.Fatalf("transaction connection changed: %q", output)
			}
			execute(database.prepare)
			if output := execute("EXECUTE agent_stmt"); output != "n\n41\n" {
				t.Fatalf("prepared statement context changed: %q", output)
			}
			execute(database.deallocate)
			cancelCtx, cancelQuery := context.WithTimeout(ctx, 100*time.Millisecond)
			defer cancelQuery()
			started := time.Now()
			result, err := conn.Execute(cancelCtx, Request{Command: database.sleep}, func(string, []byte) error { return nil })
			if err == nil || !result.Dispatched || result.Completed || !result.ContextLost || result.StopConfirmed || time.Since(started) > 3*time.Second {
				t.Fatalf("cancellation result was not bounded/uncertain: %+v %v", result, err)
			}
			if _, err = conn.Execute(ctx, Request{Command: "SELECT 1"}, func(string, []byte) error { return nil }); err != ErrContextLost {
				t.Fatal("cancelled database session was silently reused")
			}
		})
	}
}
