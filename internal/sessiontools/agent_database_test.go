package sessiontools

import (
	"net"
	"testing"
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

func TestAgentSQLWireLimitClosesTransport(t *testing.T) {
	local, remote := net.Pipe()
	defer remote.Close()
	guard := &AgentSQLTransport{conn: local}
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
