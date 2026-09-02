package agent

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jumpserver-dev/sdk-go/model"
	"github.com/jumpserver/koko/internal/agentapi"
	"github.com/jumpserver/koko/internal/agentruntime"
	"github.com/jumpserver/koko/internal/agentruntime/provider"
)

// TerminalConfig extends the SDK's common terminal settings with the current
// Core Chat AI contract. The SDK model intentionally does not yet expose these
// fields, so the embedded agent runtime decodes the endpoint into this combined value.
type TerminalConfig struct {
	model.TerminalConfig
	ChatAIEnabled  *bool  `json:"CHAT_AI_ENABLED"`
	ChatAIMethod   string `json:"CHAT_AI_METHOD"`
	ChatAIProvider string `json:"CHAT_AI_PROVIDER"`
	ChatAIBaseURL  string `json:"CHAT_AI_BASE_URL"`
	ChatAIAPIKey   string `json:"CHAT_AI_API_KEY"`
	ChatAIProxy    string `json:"CHAT_AI_PROXY"`
	ChatAIModel    string `json:"CHAT_AI_MODEL"`
}

func ProviderConfigFromTerminalConfig(value TerminalConfig) provider.Config {
	name := strings.TrimSpace(value.ChatAIProvider)
	if name == "" {
		name = provider.NameGPT
	}
	apiKey := strings.TrimSpace(value.ChatAIAPIKey)
	baseURL := strings.TrimSpace(value.ChatAIBaseURL)
	modelName := strings.TrimSpace(value.ChatAIModel)
	proxy := strings.TrimSpace(value.ChatAIProxy)
	if value.ChatAIEnabled == nil || !*value.ChatAIEnabled ||
		!strings.EqualFold(strings.TrimSpace(value.ChatAIMethod), "api") {
		apiKey, baseURL, modelName, proxy = "", "", "", ""
	}
	return provider.NormalizeConfig(provider.Config{
		Name: name, APIKey: apiKey, BaseURL: baseURL,
		Model: modelName, Proxy: proxy,
		ToolCallMode: provider.ToolCallAuto, ReasoningMode: provider.ReasoningAuto,
		ContextSoftLimitPercent: 80, RequestTimeout: 5 * time.Minute,
	})
}

func validateCreateRequest(request agentapi.CreateSessionRequest) error {
	return validateSessionRequest(request, true)
}

func validatePersistedSessionRequest(request agentapi.CreateSessionRequest) error {
	return validateSessionRequest(request, false)
}

func validateSessionRequest(request agentapi.CreateSessionRequest, requireRuntimeContracts bool) error {
	if !agentapi.ValidIdentifier(request.ResourceSessionID) {
		return fmt.Errorf("resource_session_id is invalid")
	}
	if request.ApprovalMode != "" && !validApprovalMode(request.ApprovalMode) {
		return fmt.Errorf("approval_mode is invalid")
	}
	if requireRuntimeContracts {
		if _, ok := runtimeProfilePolicyFor(request.Profile); !ok {
			return fmt.Errorf("profile is unsupported")
		}
	} else if !agentapi.ValidIdentifier(request.Profile) {
		return fmt.Errorf("profile is invalid")
	}
	if request.Revision != 1 {
		return fmt.Errorf("unsupported toolset revision")
	}
	if len(request.Tools) == 0 || len(request.Tools) > agentruntime.MaxTools {
		return fmt.Errorf("toolset size is invalid")
	}
	seen := make(map[string]struct{}, len(request.Tools))
	for _, tool := range request.Tools {
		if !agentapi.ValidIdentifier(tool.Name) {
			return fmt.Errorf("tool name is invalid")
		}
		if _, exists := seen[tool.Name]; exists {
			return fmt.Errorf("tool name is duplicated")
		}
		seen[tool.Name] = struct{}{}
		if len(tool.Description) > 4096 || !utf8.ValidString(tool.Description) {
			return fmt.Errorf("tool description is invalid")
		}
		if len(tool.Title) > 512 || !utf8.ValidString(tool.Title) || len(tool.Icons) > 8 {
			return fmt.Errorf("tool presentation metadata is invalid")
		}
		for _, icon := range tool.Icons {
			if icon.Source == "" || len(icon.Source) > 2048 ||
				(icon.Theme != "" && icon.Theme != "light" && icon.Theme != "dark") {
				return fmt.Errorf("tool icon is invalid")
			}
		}
		if len(tool.InputSchema) == 0 {
			return fmt.Errorf("tool input schema is required")
		}
		if err := validateSchema(tool.InputSchema, requireRuntimeContracts); err != nil {
			return fmt.Errorf("tool input schema is invalid: %w", err)
		}
		if err := validateSchema(tool.OutputSchema, requireRuntimeContracts); err != nil {
			return fmt.Errorf("tool output schema is invalid: %w", err)
		}
		if tool.Meta != nil {
			encoded, err := json.Marshal(tool.Meta)
			if err != nil || len(encoded) > 64*1024 {
				return fmt.Errorf("tool metadata is invalid")
			}
		}
	}
	return nil
}

func validateSchema(value json.RawMessage, compile bool) error {
	if len(value) == 0 {
		return nil
	}
	if len(value) > agentruntime.MaxToolSchemaBytes || !json.Valid(value) {
		return fmt.Errorf("schema is too large or malformed")
	}
	var object map[string]any
	if err := json.Unmarshal(value, &object); err != nil || object == nil {
		return fmt.Errorf("schema must be an object")
	}
	if !compile {
		return nil
	}
	return agentruntime.ValidateSchema(value)
}

func cloneTools(source []agentapi.ToolDefinition) []agentapi.ToolDefinition {
	result := make([]agentapi.ToolDefinition, len(source))
	for index := range source {
		result[index] = source[index]
		result[index].InputSchema = append(json.RawMessage(nil), source[index].InputSchema...)
		result[index].OutputSchema = append(json.RawMessage(nil), source[index].OutputSchema...)
		result[index].Annotations = cloneAnnotations(source[index].Annotations)
		result[index].Icons = cloneIcons(source[index].Icons)
		result[index].Meta = cloneMeta(source[index].Meta)
	}
	return result
}

func cloneIcons(source []agentapi.ToolIcon) []agentapi.ToolIcon {
	result := make([]agentapi.ToolIcon, len(source))
	for index := range source {
		result[index] = source[index]
		result[index].Sizes = append([]string(nil), source[index].Sizes...)
	}
	return result
}

func cloneMeta(source map[string]any) map[string]any {
	if source == nil {
		return nil
	}
	encoded, err := json.Marshal(source)
	if err != nil {
		return nil
	}
	var result map[string]any
	if json.Unmarshal(encoded, &result) != nil {
		return nil
	}
	return result
}

func cloneAnnotations(value agentapi.ToolAnnotations) agentapi.ToolAnnotations {
	result := value
	result.ReadOnlyHint = cloneBool(value.ReadOnlyHint)
	result.DestructiveHint = cloneBool(value.DestructiveHint)
	result.IdempotentHint = cloneBool(value.IdempotentHint)
	result.OpenWorldHint = cloneBool(value.OpenWorldHint)
	return result
}

func cloneBool(value *bool) *bool {
	if value == nil {
		return nil
	}
	result := *value
	return &result
}

func indexTools(source []agentapi.ToolDefinition) map[string]agentapi.ToolDefinition {
	result := make(map[string]agentapi.ToolDefinition, len(source))
	for _, tool := range cloneTools(source) {
		result[tool.Name] = tool
	}
	return result
}

func randomID() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(value[:]), nil
}

func boundedText(value string, limit int) string {
	value = strings.TrimSpace(value)
	if len(value) <= limit {
		return value
	}
	value = value[:limit]
	for !utf8.ValidString(value) {
		value = value[:len(value)-1]
	}
	return value
}
