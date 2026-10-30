package agentexec

import (
	"encoding/json"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func inputSchema[T any](name string) *jsonschema.Schema {
	schema, err := jsonschema.For[T](nil)
	if err != nil {
		panic(err)
	}
	for key, property := range schema.Properties {
		switch key {
		case "action_id", "session_id", "asset_id", "idempotency_key":
			property.Pattern, property.MaxLength = identifier.String(), new(128)
		case "cursor":
			property.MaxLength = new(512)
		case "command":
			property.MinLength, property.MaxLength = new(1), new(4096)
			property.Description = "Command or database statement, at most 4096 UTF-8 bytes."
		case "path", "remote_path", "destination":
			property.MinLength, property.MaxLength, property.Pattern = new(1), new(4096), "^/"
		case "content":
			property.MaxLength = new(65536)
			property.Description = "UTF-8 text of at most 65536 encoded bytes; use upload_file for larger content."
		case "sha256":
			property.Pattern = "^[0-9a-f]{64}$"
		case "size":
			property.Minimum, property.Maximum = new(float64(0)), new(float64(100<<20))
		case "timeout_seconds":
			property.Minimum, property.Maximum, property.Default = new(float64(0)), new(float64(600)), json.RawMessage("120")
			property.Description = "Execution deadline in seconds, at most 600; zero or omission uses 120."
		case "status":
			property.MaxLength = new(32)
		case "cwd":
			property.MaxLength, property.Pattern = new(4096), "^(/|$)"
		case "env":
			property.MaxProperties = new(32)
			property.PropertyNames = &jsonschema.Schema{Pattern: "^[A-Za-z_][A-Za-z0-9_]*$"}
			if property.AdditionalProperties != nil {
				property.AdditionalProperties.MaxLength = new(4096)
			}
		case "offset", "account_offset":
			maximum := float64(100000)
			if name == "get_action" {
				maximum = 1000000
			}
			property.Minimum, property.Maximum = new(float64(0)), &maximum
		case "limit":
			maximum, fallback := float64(100), "20"
			if name == "list_files" {
				maximum, fallback = 200, "100"
			} else if name == "get_action" {
				maximum, fallback = 32, "16"
			}
			property.Minimum, property.Maximum, property.Default = new(float64(0)), &maximum, json.RawMessage(fallback)
		case "overwrite":
			property.Default = json.RawMessage("false")
		}
	}
	if name == "get_action" || name == "get_session" {
		field := "action_id"
		if name == "get_session" {
			field = "session_id"
		}
		schema.OneOf = []*jsonschema.Schema{
			{Required: []string{field}, Not: &jsonschema.Schema{Required: []string{"idempotency_key"}}},
			{Required: []string{"idempotency_key"}, Not: &jsonschema.Schema{Required: []string{field}}},
		}
	}
	return schema
}

func outputSchema(name string) *jsonschema.Schema {
	text := func() *jsonschema.Schema { return &jsonschema.Schema{Type: "string"} }
	success := &jsonschema.Schema{Type: "object"}
	switch name {
	case "search_assets", "list_sessions", "list_actions":
		success.Required = []string{"results"}
		success.Properties = map[string]*jsonschema.Schema{"results": {Type: "array"}}
	case "connect_asset", "get_session", "disconnect_asset":
		success.Required = []string{"session_id"}
		success.Properties = map[string]*jsonschema.Schema{"session_id": text()}
	default:
		success.Required = []string{"action_id", "status"}
		success.Properties = map[string]*jsonschema.Schema{
			"action_id": text(), "status": text(), "next_step": text(),
			"is_terminal": {Type: "boolean"}, "output_complete": {Type: "boolean"},
			"events": {Type: "array"}, "output": {Type: "array"}, "next_cursor": text(),
		}
	}
	failure := &jsonschema.Schema{Type: "object", Required: []string{"error"}, Properties: map[string]*jsonschema.Schema{
		"error": {Type: "object", Required: []string{"code", "message", "retryable", "retry_mode", "next_step"}, Properties: map[string]*jsonschema.Schema{
			"code": text(), "message": text(), "retryable": {Type: "boolean"}, "retry_mode": text(), "next_step": text(),
		}},
	}}
	return &jsonschema.Schema{Type: "object", AnyOf: []*jsonschema.Schema{success, failure}}
}

func toolAnnotations(name string) *mcp.ToolAnnotations {
	readOnly := false
	switch name {
	case "search_assets", "get_action", "get_session", "list_sessions", "list_actions", "list_files", "stat_file", "read_file", "download_file":
		readOnly = true
	}
	return &mcp.ToolAnnotations{ReadOnlyHint: readOnly, DestructiveHint: new(!readOnly), OpenWorldHint: new(name == "execute_command" || name == "open_terminal")}
}
