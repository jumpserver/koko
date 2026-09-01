package sessiontools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"
)

const (
	MCPToolTerminalContext  = "terminal_context"
	MCPToolTerminalSnapshot = "terminal_snapshot"
	MCPToolDatabaseSchema   = "database_schema"
)

type SQLMetadataProvider interface {
	SQLMetadataScope() string
	LookupSQLSchema(context.Context, SQLSchemaLookupRequest) (SQLSchemaLookupResult, error)
	InvalidateSQLMetadata()
}

type terminalContextTool struct {
	context func() any
}

type terminalSnapshotTool struct {
	snapshot func() (any, error)
}

type databaseSchemaTool struct {
	provider SQLMetadataProvider
	guard    func() error
}

func NewTerminalContextTool(provider func() any) (MCPToolHandler, error) {
	if provider == nil {
		return nil, errors.New("terminal context provider is required")
	}
	return &terminalContextTool{context: provider}, nil
}

func NewTerminalSnapshotTool(provider func() (any, error)) (MCPToolHandler, error) {
	if provider == nil {
		return nil, errors.New("terminal snapshot provider is required")
	}
	return &terminalSnapshotTool{snapshot: provider}, nil
}

func NewDatabaseSchemaTool(
	provider SQLMetadataProvider,
	guard func() error,
) (MCPToolHandler, error) {
	if provider == nil {
		return nil, errors.New("database schema provider is required")
	}
	return &databaseSchemaTool{provider: provider, guard: guard}, nil
}

func (t *terminalContextTool) Definition() MCPToolDefinition {
	return MCPToolDefinition{
		Name:         MCPToolTerminalContext,
		Description:  "Read credential-free context for the active terminal resource",
		InputSchema:  emptyObjectSchema(),
		OutputSchema: terminalContextOutputSchema(),
		Annotations:  map[string]any{"readOnlyHint": true},
	}
}

func (t *terminalContextTool) Call(
	_ context.Context,
	arguments json.RawMessage,
) (any, error) {
	if err := requireEmptyArguments(arguments); err != nil {
		return nil, err
	}
	return t.context(), nil
}

func (t *terminalSnapshotTool) Definition() MCPToolDefinition {
	return MCPToolDefinition{
		Name:         MCPToolTerminalSnapshot,
		Title:        "Read terminal snapshot",
		Description:  "Read the bounded current terminal snapshot for AI analysis on demand",
		InputSchema:  emptyObjectSchema(),
		OutputSchema: terminalSnapshotOutputSchema(),
		// Terminal output can contain sensitive resource data. Keep the tool
		// read-only while requiring confirmation in the runtime's automatic mode.
		Annotations: map[string]any{
			"readOnlyHint": true, "openWorldHint": true,
		},
	}
}

func (t *terminalSnapshotTool) Call(
	_ context.Context,
	arguments json.RawMessage,
) (any, error) {
	if err := requireEmptyArguments(arguments); err != nil {
		return nil, err
	}
	return t.snapshot()
}

func (t *databaseSchemaTool) Definition() MCPToolDefinition {
	return MCPToolDefinition{
		Name:         MCPToolDatabaseSchema,
		Title:        "Inspect database schema",
		Description:  "List bounded table names with an empty argument object, search table names with a literal query, or describe up to five exact tables in the active database. This reads metadata only, never business rows.",
		OutputSchema: databaseSchemaOutputSchema(),
		InputSchema: map[string]any{
			"type": "object", "additionalProperties": false,
			"properties": map[string]any{
				"tables": map[string]any{
					"type": "array", "items": map[string]any{
						"type": "string", "minLength": 1, "maxLength": 256,
					},
					"maxItems":    maxSQLMetadataTables,
					"description": "Exact table names to describe; optionally qualify each name with its schema",
				},
				"query": map[string]any{
					"type": "string", "maxLength": maxSQLMetadataQuery,
					"description": "Literal case-insensitive table-name substring, not a wildcard; omit to list tables",
				},
			},
		},
		Annotations: map[string]any{
			"readOnlyHint": true, "openWorldHint": true,
		},
	}
}

func (t *databaseSchemaTool) Call(
	ctx context.Context,
	arguments json.RawMessage,
) (any, error) {
	if t.guard != nil {
		if err := t.guard(); err != nil {
			return nil, err
		}
	}
	var request SQLSchemaLookupRequest
	decoder := json.NewDecoder(bytes.NewReader(arguments))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		return nil, fmt.Errorf("decode database schema arguments: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return nil, errors.New("database schema arguments have trailing data")
	}
	lookupCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	result, err := t.provider.LookupSQLSchema(lookupCtx, request)
	if err != nil {
		return nil, err
	}
	if t.guard != nil {
		if err = t.guard(); err != nil {
			return nil, err
		}
	}
	return result, nil
}

func emptyObjectSchema() map[string]any {
	return map[string]any{
		"type": "object", "additionalProperties": false,
		"properties": map[string]any{},
	}
}

func requireEmptyArguments(arguments json.RawMessage) error {
	var value map[string]json.RawMessage
	decoder := json.NewDecoder(bytes.NewReader(arguments))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&value); err != nil {
		return err
	}
	if len(value) != 0 {
		return errors.New("tool does not accept arguments")
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return errors.New("tool arguments have trailing data")
	}
	return nil
}
