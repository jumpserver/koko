package sessiontools

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"
	"testing"
)

func TestPostgreSQLCommandToolContract(t *testing.T) {
	validator := ProtocolCommandValidator("postgresql")
	if _, err := validator("free -h"); err == nil || !strings.Contains(err.Error(), "SQL statements only") {
		t.Fatalf("shell command validation error = %v", err)
	}
	if _, err := validator("SELECT current_setting('shared_buffers')"); err != nil {
		t.Fatalf("valid PostgreSQL statement rejected: %v", err)
	}

	handler, err := NewCommandTool(MCPCommandToolOptions{
		Protocol: "postgresql", Validate: validator,
		Hooks: MCPCommandHooks{PTYExecute: func(
			context.Context, string, *CommandACLDecision,
		) (string, *int, error) {
			return "", nil, nil
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	definition := handler.Definition()
	if definition.Name != MCPToolExecuteSQL || definition.Title != "Execute PostgreSQL SQL" ||
		!strings.Contains(definition.Description, "shell commands are unavailable") {
		t.Fatalf("unexpected PostgreSQL tool presentation: %#v", definition)
	}
	properties := definition.InputSchema["properties"].(map[string]any)
	patternValue := properties["command"].(map[string]any)["pattern"]
	pattern, ok := patternValue.(string)
	if !ok {
		t.Fatalf("command schema pattern = %#v", patternValue)
	}
	compiledPattern, err := regexp.Compile(pattern)
	if err != nil {
		t.Fatalf("compile command schema pattern: %v", err)
	}
	if !compiledPattern.MatchString("SELECT 1") {
		t.Fatal("command schema rejected a valid single-line command")
	}
	for _, command := range []string{
		"SELECT 1\nSELECT 2",
		"\tSELECT 1",
		"SELECT 1\u0085",
		"SELECT 1\u2028SELECT 2",
		"SELECT 1\u2029SELECT 2",
	} {
		if compiledPattern.MatchString(command) {
			t.Fatalf("command schema accepted %q", command)
		}
		payload, marshalErr := json.Marshal(map[string]string{"command": command})
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		if _, callErr := handler.Call(context.Background(), payload); callErr == nil ||
			!strings.Contains(callErr.Error(), "line breaks or control characters") {
			t.Fatalf("command %q validation error = %v", command, callErr)
		}
	}
	modes := properties["execution"].(map[string]any)["enum"].([]string)
	if len(modes) != 2 || modes[0] != MCPExecutionAuto || modes[1] != MCPExecutionPTY {
		t.Fatalf("execution modes = %#v", modes)
	}
}

func TestDatabaseSchemaArgumentsSupportBoundedListing(t *testing.T) {
	for _, request := range []SQLSchemaLookupRequest{{}, {Query: "*"}} {
		normalized, err := normalizeSQLSchemaLookupRequest(request)
		if err != nil {
			t.Fatal(err)
		}
		if normalized.Query != "" || len(normalized.Tables) != 0 {
			t.Fatalf("normalized request = %#v", normalized)
		}
	}
}

func TestSessionSpecificToolSelection(t *testing.T) {
	for protocol, expected := range map[string]string{
		"ssh":        MCPToolExecuteShell,
		"k8s":        MCPToolExecuteShell,
		"postgresql": MCPToolExecuteSQL,
		"redis":      MCPToolExecuteRedis,
		"mongodb":    MCPToolExecuteMongoDB,
	} {
		if actual := commandToolName(protocol); actual != expected {
			t.Fatalf("command tool for %s = %s, want %s", protocol, actual, expected)
		}
	}

	handlers, err := NewFileToolHandlers(
		struct{ FileExecutor }{},
		FileToolCapabilities{ReadText: true},
	)
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(handlers))
	for _, handler := range handlers {
		names = append(names, handler.Definition().Name)
	}
	joined := strings.Join(names, ",")
	if joined != "list_directory,stat,read_text" {
		t.Fatalf("read-only file tools = %s", joined)
	}
}

func TestAgentToolOutputSchemasAndLargeResult(t *testing.T) {
	schemas := []map[string]any{
		commandOutputSchema(), terminalContextOutputSchema(),
		terminalSnapshotOutputSchema(), databaseSchemaOutputSchema(),
	}
	for _, name := range []string{
		ToolListDirectory, ToolStat, ToolReadText, ToolSaveText,
		ToolMkdir, ToolRename, ToolDelete,
	} {
		schemas = append(schemas, fileToolOutputSchema(name))
	}
	for _, schema := range schemas {
		encoded, err := json.Marshal(schema)
		if err != nil {
			t.Fatal(err)
		}
		if err = ValidateSchema(encoded); err != nil {
			t.Fatalf("invalid output schema: %v", err)
		}
	}

	result, payload := newMCPCallToolResult(map[string]any{
		"output": strings.Repeat("x", 100*1024),
	}, nil)
	if len(payload) == 0 || len(payload) > MaxToolResultBytes {
		t.Fatalf("large structured result has invalid wire size %d", len(payload))
	}
	if result.StructuredContent == nil || len(result.Content) != 1 ||
		len(result.Content[0].Text) > maxMCPTextResultBytes {
		t.Fatal("large structured result was not preserved with a bounded text fallback")
	}
}

func TestMCPAgentBindingRequiresRevision(t *testing.T) {
	meta := map[string]json.RawMessage{
		MCPAgentMetaKey: json.RawMessage(`{"resource_session_id":"resource-1","tool_call_id":"call-1"}`),
		"io.modelcontextprotocol/protocolVersion":    json.RawMessage(`"2026-07-28"`),
		"io.modelcontextprotocol/clientCapabilities": json.RawMessage(`{}`),
	}
	if _, err := decodeMCPAgentBinding(meta, true); err == nil {
		t.Fatal("binding without a toolset revision was accepted")
	}
}

func TestMCPAgentBindingIgnoresUnknownFields(t *testing.T) {
	meta := map[string]json.RawMessage{
		MCPAgentMetaKey: json.RawMessage(`{"resource_session_id":"resource-1","tool_call_id":"call-1","revision":1,"registration_id":"registration-1","invocation_id":"invocation-1","definition_version":"1","definition_digest":"digest-1"}`),
		"io.modelcontextprotocol/protocolVersion":    json.RawMessage(`"2026-07-28"`),
		"io.modelcontextprotocol/clientCapabilities": json.RawMessage(`{}`),
	}
	binding, err := decodeMCPAgentBinding(meta, true)
	if err != nil {
		t.Fatal(err)
	}
	if binding.ResourceSessionID != "resource-1" || binding.ToolCallID != "call-1" || binding.Revision != 1 {
		t.Fatalf("unexpected binding: %#v", binding)
	}
}
