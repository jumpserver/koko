package terminalai

import (
	"strings"
	"testing"

	"github.com/jumpserver-dev/sdk-go/model"
)

func TestResolveAdapter(t *testing.T) {
	tests := []struct {
		name       string
		context    SessionContext
		adapter    string
		background bool
	}{
		{
			name: "generic telnet", context: SessionContext{Protocol: "telnet"},
			adapter: "terminal",
		},
		{
			name: "network ssh", context: SessionContext{
				Protocol: "ssh", PlatformType: "Cisco", BaseOS: "linux",
			},
			adapter: "terminal",
		},
		{
			name: "linux ssh", context: SessionContext{
				Protocol: "ssh", BaseOS: "linux",
			},
			adapter: "ssh-shell", background: true,
		},
		{
			name: "unix ssh", context: SessionContext{
				Protocol: "ssh", PlatformType: "Unix",
			},
			adapter: "ssh-shell", background: true,
		},
		{
			name: "mysql", context: SessionContext{Protocol: "MYSQL"},
			adapter: "mysql", background: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			adapter := ResolveAdapter(test.context)
			if adapter.Name() != test.adapter {
				t.Fatalf("adapter = %q, want %q", adapter.Name(), test.adapter)
			}
			if adapter.SupportsBackground() != test.background {
				t.Fatalf(
					"background = %t, want %t",
					adapter.SupportsBackground(), test.background,
				)
			}
		})
	}
}

func TestAnalyzeSQLBackgroundPolicy(t *testing.T) {
	tests := []struct {
		name      string
		statement string
		eligible  bool
		kind      sqlKind
		keyword   string
	}{
		{
			name: "read", statement: "SELECT ';' AS value;",
			eligible: true, kind: sqlRead, keyword: "SELECT",
		},
		{
			name: "cte update", statement: "WITH c AS (SELECT id FROM t) UPDATE t SET v = 1 WHERE id IN (SELECT id FROM c)",
			eligible: true, kind: sqlWrite, keyword: "UPDATE",
		},
		{
			name: "multiple", statement: "SELECT 1; SELECT 2",
			kind: sqlRead, keyword: "SELECT",
		},
		{
			name: "multiple after minus", statement: "SELECT 1--1; SELECT 2",
			kind: sqlRead, keyword: "SELECT",
		},
		{
			name: "session", statement: "SET sql_safe_updates = 1",
			kind: sqlSession, keyword: "SET",
		},
		{
			name: "temporary table", statement: "CREATE TEMPORARY TABLE x (id int)",
			kind: sqlSession, keyword: "CREATE",
		},
		{
			name: "locking read", statement: "SELECT id FROM jobs FOR UPDATE",
			kind: sqlSession, keyword: "SELECT",
		},
		{
			name: "unknown", statement: "DO SLEEP(1)",
			kind: sqlUnknown, keyword: "DO",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			analysis, err := analyzeSQL(test.statement)
			if err != nil {
				t.Fatal(err)
			}
			if analysis.BackgroundEligible() != test.eligible {
				t.Fatalf(
					"eligible = %t, want %t", analysis.BackgroundEligible(), test.eligible,
				)
			}
			if analysis.kind != test.kind || analysis.keyword != test.keyword {
				t.Fatalf(
					"analysis = (%d, %q), want (%d, %q)",
					analysis.kind, analysis.keyword, test.kind, test.keyword,
				)
			}
		})
	}
}

func TestMySQLAdapterRiskAndFallback(t *testing.T) {
	adapter := ResolveAdapter(SessionContext{Protocol: "mysql"})
	tests := []struct {
		command   string
		execution string
		risk      int
		eligible  bool
	}{
		{
			command: "SELECT id FROM users", execution: ExecutionBackground,
			risk: 1, eligible: true,
		},
		{
			command:   "INSERT INTO users(name) VALUES ('a')",
			execution: ExecutionBackground, risk: 2, eligible: true,
		},
		{
			command:   "UPDATE users SET active = 0",
			execution: ExecutionBackground, risk: 4, eligible: true,
		},
		{
			command: "WITH c AS (SELECT id FROM users WHERE active = 1) " +
				"UPDATE users SET active = 0",
			execution: ExecutionBackground, risk: 4, eligible: true,
		},
		{
			command: "USE reporting", execution: ExecutionPTY,
			risk: 2,
		},
		{
			command: "SET PASSWORD = 'secret'", execution: ExecutionPTY,
			risk: 4,
		},
	}
	for _, test := range tests {
		proposal := CommandProposal{
			Command: test.command, Execution: ExecutionBackground,
			RiskLevel: 1,
		}
		if err := adapter.PrepareProposal(&proposal); err != nil {
			t.Fatal(err)
		}
		if proposal.Execution != test.execution ||
			proposal.RiskLevel != test.risk ||
			proposal.BackgroundEligible != test.eligible {
			t.Fatalf(
				"%q => execution=%s risk=%d eligible=%t",
				test.command, proposal.Execution, proposal.RiskLevel,
				proposal.BackgroundEligible,
			)
		}
	}
}

func TestMySQLSanitizer(t *testing.T) {
	sanitizer := NewMySQLSanitizer([]model.DataMaskingRule{
		{
			FieldsPattern: "phone,secret_*", MaskingMethod: "hide_middle",
			MaskPattern: "####", Priority: 1, IsActive: true,
		},
		{
			FieldsPattern: "phone", MaskingMethod: "keep_suffix",
			Priority: 10, IsActive: true,
		},
		{
			FieldsPattern: "ignored", MaskingMethod: "fixed_char",
			MaskPattern: "masked", Priority: 100, IsActive: false,
		},
	})
	if value := sanitizer.Sanitize("PHONE", "13800138000"); value != "*********00" {
		t.Fatalf("phone = %q", value)
	}
	if value := sanitizer.Sanitize("secret_token", "abcdef"); value != "a****f" {
		t.Fatalf("secret = %q", value)
	}
	if value := sanitizer.Sanitize("ignored", "visible"); value != "visible" {
		t.Fatalf("inactive rule changed value to %q", value)
	}
}

func TestMySQLOutputLimit(t *testing.T) {
	output := newMySQLOutput()
	line := strings.Repeat("x", maxMySQLOutput+1)
	if output.addLine(line) {
		t.Fatal("oversized line was accepted")
	}
	value := output.String()
	if len(value) > maxMySQLOutput {
		t.Fatalf("output size = %d", len(value))
	}
	if !strings.Contains(value, "output truncated") {
		t.Fatalf("missing truncation marker: %q", value)
	}
}
