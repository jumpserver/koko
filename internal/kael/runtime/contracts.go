package runtime

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/jumpserver/koko/internal/kael/domain"
	"github.com/jumpserver/koko/internal/kael/model"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

type toolContract struct {
	registration domain.Registration
	providerName string
	input        *jsonschema.Schema
	output       *jsonschema.Schema
}

func compileTools(registrations []domain.Registration) (map[string]toolContract, []model.Tool, error) {
	contracts := make(map[string]toolContract, len(registrations))
	tools := make([]model.Tool, 0, len(registrations))
	used := map[string]string{}
	for _, registration := range registrations {
		input, err := compileSchema(registration.InputSchema)
		if err != nil {
			return nil, nil, fmt.Errorf("compile %s input: %w", registration.Name, err)
		}
		var output *jsonschema.Schema
		if len(registration.OutputSchema) > 0 {
			output, err = compileSchema(registration.OutputSchema)
			if err != nil {
				return nil, nil, fmt.Errorf("compile %s output: %w", registration.Name, err)
			}
		}
		providerName := safeToolName("kael_" + registration.Name)
		if existing := used[providerName]; existing != "" {
			return nil, nil, fmt.Errorf("tool aliases conflict")
		}
		used[providerName] = registration.Name
		contract := toolContract{registration: registration, providerName: providerName, input: input, output: output}
		contracts[providerName] = contract
		tools = append(tools, toolForContract(contract))
	}
	return contracts, tools, nil
}

func toolForContract(contract toolContract) model.Tool {
	var parameters map[string]any
	_ = json.Unmarshal(contract.registration.InputSchema, &parameters)
	return model.Tool{Name: contract.providerName, Description: contract.registration.Description, Parameters: parameters}
}

func compileSchema(raw json.RawMessage) (*jsonschema.Schema, error) {
	if len(raw) == 0 || len(raw) > domain.MaxToolSchemaBytes {
		return nil, fmt.Errorf("schema size is invalid")
	}
	compiler := jsonschema.NewCompiler()
	compiler.AssertFormat()
	document, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	if err = compiler.AddResource("schema.json", document); err != nil {
		return nil, err
	}
	return compiler.Compile("schema.json")
}
func ValidateSchema(raw json.RawMessage) error { _, err := compileSchema(raw); return err }
func validateJSON(schema *jsonschema.Schema, raw json.RawMessage) error {
	var value any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return err
	}
	return schema.Validate(value)
}
func canonicalArguments(raw json.RawMessage) (json.RawMessage, error) {
	if len(raw) == 0 || len(raw) > domain.MaxToolArgumentsBytes {
		return nil, fmt.Errorf("tool arguments size is invalid")
	}
	trimmed := bytes.TrimSpace(raw)
	if bytes.HasPrefix(trimmed, []byte("```")) {
		trimmed = bytes.TrimPrefix(trimmed, []byte("```json"))
		trimmed = bytes.TrimPrefix(trimmed, []byte("```"))
		trimmed = bytes.TrimSuffix(trimmed, []byte("```"))
		trimmed = bytes.TrimSpace(trimmed)
	}
	var value map[string]any
	decoder := json.NewDecoder(bytes.NewReader(trimmed))
	decoder.UseNumber()
	if decoder.Decode(&value) != nil || value == nil {
		return nil, fmt.Errorf("tool arguments must be an object")
	}
	var trailing any
	if decoder.Decode(&trailing) != io.EOF {
		return nil, fmt.Errorf("tool arguments contain trailing data")
	}
	encoded, err := domain.CanonicalJSON(value)
	return json.RawMessage(encoded), err
}
func toolFingerprint(registration domain.Registration, arguments json.RawMessage) string {
	if registration.BindingKind == "service" && registration.Name == "call_core_api" && strings.HasPrefix(registration.ExecutionBindingID, "platform-gateway:") {
		// These fields only describe progress in the UI. Changing their wording
		// must not allow the same Core write to execute again.
		var values map[string]json.RawMessage
		if json.Unmarshal(arguments, &values) == nil && values != nil {
			delete(values, "progress")
			delete(values, "action")
			if encoded, err := json.Marshal(values); err == nil {
				arguments = encoded
			}
		}
	}
	sum := sha256.Sum256(append([]byte(registration.ID+"\x00"), arguments...))
	return hex.EncodeToString(sum[:])
}

var unsafeName = regexp.MustCompile(`[^A-Za-z0-9_-]+`)

func safeToolName(value string) string {
	value = unsafeName.ReplaceAllString(value, "_")
	if value == "" || value[0] >= '0' && value[0] <= '9' {
		value = "tool_" + value
	}
	if len(value) > 64 {
		hash := sha256.Sum256([]byte(value))
		value = value[:48] + "_" + hex.EncodeToString(hash[:7])
	}
	return value
}
func buildMessages(input Input) ([]model.Message, int, error) {
	messages := input.Messages

	result := make([]model.Message, 0, len(messages))
	size := 0
	for _, message := range messages {
		if !utf8.ValidString(message.Content) {
			return nil, 0, fmt.Errorf("message is invalid")
		}
		role := message.Role
		if role != "assistant" && role != "user" && role != "tool" {
			continue
		}
		parts := []model.ContentPart{}
		var messageParts []domain.MessagePart
		if json.Unmarshal(message.Parts, &messageParts) == nil {
			for _, part := range messageParts {
				switch part.Type {
				case "text":
					parts = append(parts, model.ContentPart{Type: "text", Text: bounded(part.Text, domain.MaxMessageBytes)})
					size += len(part.Text)
				case "artifact":
					if artifacts, ok := input.Artifacts[part.ArtifactID]; ok {
						parts = append(parts, artifacts...)
						for _, artifact := range artifacts {
							size += len(artifact.Text) + len(artifact.Data)
						}
					}
				}
			}
		}
		if len(parts) == 0 {
			size += len(message.Content)
		}
		result = append(result, model.Message{Role: role, Content: bounded(message.Content, domain.MaxMessageBytes), Parts: parts})
	}
	if input.Context != nil {
		size += len(input.Context.Data)
	}
	return result, size, nil
}
func bounded(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	value = value[:limit]
	for !utf8.ValidString(value) {
		value = value[:len(value)-1]
	}
	return value
}
