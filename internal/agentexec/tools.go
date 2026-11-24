package agentexec

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type toolOperation struct {
	operation string
	schema    *jsonschema.Schema
	run       func(context.Context, Principal, json.RawMessage) (any, error)
}

func operationFor[T any](operation string, fn func(context.Context, Principal, T) (any, error)) toolOperation {
	return toolOperation{operation: operation, schema: inputSchema[T](), run: func(ctx context.Context, p Principal, args json.RawMessage) (any, error) {
		var input T
		if err := json.Unmarshal(args, &input); err != nil {
			return nil, err
		}
		return fn(ctx, p, input)
	}}
}

func registerToolGroup(server *mcp.Server, issuer, name, description string, operations ...toolOperation) {
	schema := &jsonschema.Schema{Type: "object"}
	for _, operation := range operations {
		branches := operation.schema.OneOf
		if len(branches) == 0 {
			branches = []*jsonschema.Schema{operation.schema}
		}
		for _, branch := range branches {
			variant := branch.CloneSchemas()
			variant.Properties["operation"] = &jsonschema.Schema{Type: "string", Enum: []any{operation.operation}}
			variant.Required = append([]string{"operation"}, branch.Required...)
			schema.OneOf = append(schema.OneOf, variant)
		}
	}
	registerToolWithSchema(server, issuer, name, description, schema, func(ctx context.Context, p Principal, args json.RawMessage) (any, error) {
		var selector struct {
			Operation string `json:"operation"`
		}
		if err := json.Unmarshal(args, &selector); err != nil {
			return nil, err
		}
		for _, operation := range operations {
			if operation.operation == selector.Operation {
				return operation.run(ctx, p, args)
			}
		}
		return nil, errors.New("unsupported operation")
	})
}
