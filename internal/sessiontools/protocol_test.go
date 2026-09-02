package sessiontools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/jumpserver/koko/internal/agentapi"
	"github.com/jumpserver/koko/internal/agentruntime"
)

func TestPostgreSQLCommandToolContractRejectsShell(t *testing.T) {
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
		if err = agentruntime.ValidateSchema(encoded); err != nil {
			t.Fatalf("invalid output schema: %v", err)
		}
	}

	result, payload := newMCPCallToolResult(map[string]any{
		"output": strings.Repeat("x", 100*1024),
	}, nil)
	if len(payload) == 0 || len(payload) > agentapi.MaxToolResultBytes {
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
