package sessiontools

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/jumpserver/koko/internal/agentapi"
	"github.com/jumpserver/koko/internal/agentruntime"
)

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
