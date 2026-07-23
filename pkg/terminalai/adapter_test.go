package terminalai

import (
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"

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

func TestProtocolPlatformProfiles(t *testing.T) {
	tests := []struct {
		name     string
		context  SessionContext
		adapter  string
		family   string
		language string
	}{
		{
			name: "cisco ssh",
			context: SessionContext{
				Protocol: "ssh", PlatformType: "Cisco", BaseOS: "linux",
			},
			adapter: "terminal", family: "cisco-ios", language: "Cisco IOS",
		},
		{
			name: "huawei telnet",
			context: SessionContext{
				Protocol: "telnet", PlatformName: "Huawei VRP",
			},
			adapter: "terminal", family: "huawei-vrp", language: "Huawei VRP",
		},
		{
			name: "h3c ssh",
			context: SessionContext{
				Protocol: "ssh", PlatformName: "H3C Comware",
			},
			adapter: "terminal", family: "h3c-comware", language: "H3C Comware",
		},
		{
			name: "juniper ssh",
			context: SessionContext{
				Protocol: "ssh", PlatformName: "Juniper Junos",
			},
			adapter: "terminal", family: "juniper-junos", language: "Juniper Junos",
		},
		{
			name: "windows ssh",
			context: SessionContext{
				Protocol: "ssh", PlatformType: "Windows",
			},
			adapter: "terminal", family: "windows", language: "PowerShell",
		},
		{
			name: "postgresql",
			context: SessionContext{
				Protocol: "postgresql", PlatformType: "PostgreSQL",
			},
			adapter: "postgresql", family: "postgresql",
			language: "PostgreSQL SQL",
		},
		{
			name:    "redis",
			context: SessionContext{Protocol: "redis"},
			adapter: "redis", family: "redis", language: "Redis command",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			profile := ResolveAdapter(test.context).Profile()
			if profile.Adapter != test.adapter ||
				profile.PlatformFamily != test.family ||
				!strings.Contains(profile.CommandLanguage, test.language) {
				t.Fatalf(
					"profile = (%q, %q, %q)",
					profile.Adapter, profile.PlatformFamily, profile.CommandLanguage,
				)
			}
		})
	}
}

func TestRegisterProtocol(t *testing.T) {
	const protocol = "terminal-ai-test"
	err := RegisterProtocol(ProtocolRegistration{
		Protocol: protocol,
		NewAdapter: func(context SessionContext) Adapter {
			return &terminalAdapter{
				context: context, name: protocol,
				commandLanguage: "test command language",
			}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err = RegisterProtocol(ProtocolRegistration{
		Protocol: protocol,
		NewAdapter: func(context SessionContext) Adapter {
			return &terminalAdapter{context: context}
		},
	}); err == nil {
		t.Fatal("duplicate protocol registration was accepted")
	}
	if adapter := ResolveAdapter(SessionContext{Protocol: protocol}); adapter.Name() != protocol {
		t.Fatalf("adapter = %q", adapter.Name())
	}
	if SupportsBackground(SessionContext{Protocol: protocol}) {
		t.Fatal("protocol without executor reported background support")
	}
}

func TestBuiltInProtocolRegistry(t *testing.T) {
	registered := strings.Join(RegisteredProtocols(), ",")
	for _, protocol := range []string{
		"ssh", "telnet", "mysql", "mariadb", "postgresql", "sqlserver",
		"oracle", "clickhouse", "redis", "mongodb",
	} {
		if !strings.Contains(","+registered+",", ","+protocol+",") {
			t.Fatalf("protocol %q is not registered: %s", protocol, registered)
		}
	}
	tests := []struct {
		context  SessionContext
		expected bool
	}{
		{context: SessionContext{Protocol: "ssh", BaseOS: "linux"}, expected: true},
		{context: SessionContext{Protocol: "ssh", PlatformType: "Cisco"}},
		{context: SessionContext{Protocol: "mysql"}, expected: true},
		{context: SessionContext{Protocol: "postgresql"}},
	}
	for _, test := range tests {
		if value := SupportsBackground(test.context); value != test.expected {
			t.Fatalf(
				"SupportsBackground(%+v) = %t, want %t",
				test.context, value, test.expected,
			)
		}
	}
}

func TestSessionContextExcludesCredentials(t *testing.T) {
	token := &model.ConnectToken{
		Protocol: "ssh",
		Asset: model.Asset{
			Name: "asset", SpecInfo: model.SpecInfo{DBName: "database"},
		},
		Account: model.Account{BaseAccount: model.BaseAccount{
			Username: "credential-user", Secret: "credential-secret",
		}},
		Platform: model.Platform{
			Name:     "Linux",
			Category: model.LabelValue{Value: "host"},
			Type:     model.LabelValue{Value: "linux"},
			Charset:  model.LabelValue{Value: "utf-8"},
		},
	}
	value, err := json.Marshal(NewSessionContext(token))
	if err != nil {
		t.Fatal(err)
	}
	text := string(value)
	if strings.Contains(text, "credential-user") ||
		strings.Contains(text, "credential-secret") {
		t.Fatalf("session context contains account credentials: %s", text)
	}
	for _, expected := range []string{"asset", "host", "linux", "utf-8", "database"} {
		if !strings.Contains(text, expected) {
			t.Fatalf("session context is missing %q: %s", expected, text)
		}
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
		approval  bool
	}{
		{
			command: "SELECT id FROM users", execution: ExecutionBackground,
			risk: 1, eligible: true,
		},
		{
			command:   "INSERT INTO users(name) VALUES ('a')",
			execution: ExecutionBackground, risk: 2, eligible: true, approval: true,
		},
		{
			command:   "UPDATE users SET active = 0",
			execution: ExecutionBackground, risk: 4, eligible: true, approval: true,
		},
		{
			command: "WITH c AS (SELECT id FROM users WHERE active = 1) " +
				"UPDATE users SET active = 0",
			execution: ExecutionBackground, risk: 4, eligible: true, approval: true,
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
			proposal.BackgroundEligible != test.eligible ||
			proposal.ApprovalRequired != test.approval {
			t.Fatalf(
				"%q => execution=%s risk=%d eligible=%t approval=%t",
				test.command, proposal.Execution, proposal.RiskLevel,
				proposal.BackgroundEligible,
				proposal.ApprovalRequired,
			)
		}
	}
}

func TestSQLAdaptersRejectMultipleStatements(t *testing.T) {
	for _, protocol := range []string{"mysql", "mariadb", "postgresql"} {
		proposal := CommandProposal{
			Command:   "SELECT 1; SELECT 2",
			Execution: ExecutionPTY, RiskLevel: 1,
		}
		if err := ResolveAdapter(SessionContext{
			Protocol: protocol,
		}).PrepareProposal(&proposal); err == nil {
			t.Fatalf("%s adapter accepted multiple statements", protocol)
		}
	}
}

func TestSQLWritesAlwaysRequireApproval(t *testing.T) {
	for _, command := range []string{
		"UPDATE users SET active = 0 WHERE id = 1",
		"CALL refresh_users()",
		"EXPLAIN ANALYZE DELETE FROM users WHERE id = 1",
	} {
		proposal := CommandProposal{
			Command: command, Execution: ExecutionPTY, RiskLevel: 1,
		}
		if err := ResolveAdapter(SessionContext{
			Protocol: "postgresql",
		}).PrepareProposal(&proposal); err != nil {
			t.Fatal(err)
		}
		if !proposal.ApprovalRequired {
			t.Fatalf("%q did not require approval", command)
		}
		if !requiresRiskApproval(proposal, 4, false) {
			t.Fatalf("%q bypassed approval at threshold 4", command)
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

func TestPromptContextLimitIsUTF8Safe(t *testing.T) {
	value := strings.Repeat("终端输出", 10000)
	result := promptTail(value, 4096)
	if len(result) > 4096 {
		t.Fatalf("limited prompt length = %d", len(result))
	}
	if !utf8.ValidString(result) {
		t.Fatal("limited prompt is not valid UTF-8")
	}
	if !strings.HasPrefix(result, truncatedPromptMarker) {
		t.Fatal("limited prompt is missing truncation marker")
	}
	results := compactResults([]StepResult{{Output: value}})
	if len(results[0].Output) > maxModelResultOutput {
		t.Fatalf("result output length = %d", len(results[0].Output))
	}
}

func TestModelOutputLimits(t *testing.T) {
	if err := validateDecision(Decision{
		Kind: "plan",
		Steps: []Step{{
			Title: "check", Objective: strings.Repeat("x", maxStepObjective+1),
		}},
	}); err == nil {
		t.Fatal("oversized plan step was accepted")
	}
	proposal := CommandProposal{
		Command: "pwd", Execution: ExecutionPTY, RiskLevel: 1,
		Rationale: strings.Repeat("x", maxProposalExplanation+1),
	}
	if err := validateProposal(&proposal); err == nil {
		t.Fatal("oversized proposal metadata was accepted")
	}
	if err := validateStepReview(StepReview{Outcome: "unknown"}); err == nil {
		t.Fatal("invalid review outcome was accepted")
	}
}
