package agentruntime

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

const schemaResourceURL = "https://agent.invalid/tool-schema.json"

type rejectingSchemaLoader struct{}

func (rejectingSchemaLoader) Load(url string) (any, error) {
	return nil, fmt.Errorf("external schema reference %q is not allowed", url)
}

func compileSchema(raw json.RawMessage) (*jsonschema.Schema, error) {
	if len(raw) == 0 || len(raw) > MaxToolSchemaBytes {
		return nil, fmt.Errorf("schema size is invalid")
	}
	value, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("decode JSON schema: %w", err)
	}
	if _, ok := value.(map[string]any); !ok {
		return nil, fmt.Errorf("schema must be an object")
	}
	compiler := jsonschema.NewCompiler()
	compiler.DefaultDraft(jsonschema.Draft2020)
	compiler.UseLoader(rejectingSchemaLoader{})
	if err = compiler.AddResource(schemaResourceURL, value); err != nil {
		return nil, fmt.Errorf("load JSON schema: %w", err)
	}
	schema, err := compiler.Compile(schemaResourceURL)
	if err != nil {
		return nil, fmt.Errorf("compile JSON schema: %w", err)
	}
	return schema, nil
}

// ValidateSchema verifies a complete JSON Schema without resolving references
// outside the schema document. Agentd uses it before accepting a toolset, and
// Runtime compiles the same contract once for execution-time validation.
func ValidateSchema(raw json.RawMessage) error {
	if len(raw) == 0 {
		return nil
	}
	_, err := compileSchema(raw)
	return err
}

func validateJSON(schema *jsonschema.Schema, raw json.RawMessage) error {
	value, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		return fmt.Errorf("decode JSON value: %w", err)
	}
	if err = schema.Validate(value); err != nil {
		return fmt.Errorf("does not match JSON Schema: %w", err)
	}
	return nil
}
