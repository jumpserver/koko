package sessiontools

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"
)

func TestAgentSQLAllowsSessionStatementsButNotBatches(t *testing.T) {
	for _, protocol := range []string{"mysql", "postgresql"} {
		for _, command := range []string{"BEGIN", "ROLLBACK", "CREATE TEMPORARY TABLE agent_test (id int)", "SELECT 1 FOR UPDATE"} {
			if _, err := AgentSQLReturnsRows(command, protocol); err != nil {
				t.Fatalf("%s %s: %v", protocol, command, err)
			}
		}
		if _, err := AgentSQLReturnsRows("SELECT 1; DELETE FROM agent_test", protocol); err == nil {
			t.Fatal("SQL batch accepted")
		}
	}
	if rows, err := AgentSQLReturnsRows("UPDATE agent_test SET id=2 RETURNING id", "postgresql"); err != nil || !rows {
		t.Fatalf("RETURNING result lost: %v", err)
	}
}

func TestAgentNativeSQLUsesBoundedDialer(t *testing.T) {
	for _, config := range []DatabaseConfig{
		{Protocol: "sqlserver"}, {Protocol: "oracle"}, {Protocol: "clickhouse"},
		{Protocol: "mysql", PlatformType: "oceanbase", TenantMode: "mysql"},
		{Protocol: "mysql", PlatformType: "oceanbase", TenantMode: "oracle"},
	} {
		t.Run(config.Protocol+config.PlatformType+config.TenantMode, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			called := false
			config.Host, config.Port, config.ServerName = "asset.private.invalid", 1234, "asset.example"
			config.Username, config.Password, config.Database = "agent", "test", "agent"
			config.DialContext = func(_ context.Context, _, address string) (net.Conn, error) {
				called = true
				if address != "asset.private.invalid:1234" {
					t.Errorf("dialed outside configured endpoint: %s", address)
				}
				return nil, errors.New("test connection refused")
			}
			_, _, _, err := OpenAgentSQLConnection(ctx, config)
			if err == nil || !called {
				t.Fatalf("native protocol did not use its transport: %v", err)
			}
		})
	}
}

func TestAgentOceanBaseRejectsUnknownTenantBeforeDial(t *testing.T) {
	for _, mode := range []string{"", "unknown"} {
		_, _, _, err := OpenAgentSQLConnection(context.Background(), DatabaseConfig{
			Protocol: "mysql", PlatformType: "oceanbase", TenantMode: mode,
			DialContext: func(context.Context, string, string) (net.Conn, error) {
				t.Fatal("unknown OceanBase tenant reached the network")
				return nil, nil
			},
		})
		if err == nil {
			t.Fatal("unknown tenant was accepted")
		}
	}
}

func TestAgentSQLRecognizesNewProtocolResults(t *testing.T) {
	for _, tc := range []struct{ protocol, command string }{
		{"sqlserver", "UPDATE agent SET id=2 OUTPUT inserted.id"},
		{"sqlserver", "EXEC agent_proc"},
		{"oracle", "SELECT 1 FROM dual"},
		{"clickhouse", "EXISTS TABLE agent"},
	} {
		if rows, err := AgentSQLReturnsRows(tc.command, tc.protocol); err != nil || !rows {
			t.Fatalf("%s result set was lost: %v", tc.protocol, err)
		}
	}
}

func TestAgentSQLWireLimitClosesTransport(t *testing.T) {
	local, remote := net.Pipe()
	defer remote.Close()
	guard := &AgentSQLTransport{connections: map[net.Conn]struct{}{local: {}}}
	guard.remaining.Store(3)
	conn := &agentSQLWireConn{Conn: local, guard: guard}
	done := make(chan struct{})
	go func() { defer close(done); _, _ = remote.Write([]byte("abcd")) }()
	var buffer [8]byte
	if n, err := conn.Read(buffer[:]); n != 3 || err != nil {
		t.Fatalf("wire budget not applied: %d %v", n, err)
	}
	if _, err := conn.Read(buffer[:]); err == nil {
		t.Fatal("oversized protocol response accepted")
	}
	<-done
}
