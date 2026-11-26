package platformgateway

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jumpserver/koko/internal/kael/domain"
	"github.com/jumpserver/koko/internal/kael/identity"
	"github.com/jumpserver/koko/internal/kael/ports"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

const (
	searchTool = "search_core_api"
	callTool   = "call_core_api"
)

type Config struct {
	CoreURL        string
	CoreTLSVerify  bool
	CACert         string
	ClientCert     string
	ClientKey      string
	AllowedMethods map[string]bool
	Timeout        time.Duration
	MaxResponse    int64
	OpenAPILoader  func(context.Context) (map[string]any, error)
}

type parameter struct {
	Name     string         `json:"name"`
	Required bool           `json:"required"`
	Style    string         `json:"style,omitempty"`
	Explode  bool           `json:"explode"`
	Schema   map[string]any `json:"schema,omitempty"`
}

type reuseField struct {
	BodyField      string
	QueryParameter string
	Unwrap         string
	Default        any
	HasDefault     bool
}

type createReusePolicy struct {
	LookupOperationID string
	Fields            []reuseField
}

type operation struct {
	ID                  string
	Method              string
	Path                string
	Summary             string
	Description         string
	Guidance            string
	Tags                []string
	PathParams          []parameter
	QueryParams         []parameter
	Body                map[string]any
	BodyRequired        bool
	RequiredPermissions []string
	PermissionDynamic   bool
	RBACProtected       bool
	SafeSensitivePath   bool
	CreateReuse         *createReusePolicy
}

type secretInputField struct {
	Name     string `json:"name"`
	Required bool   `json:"required"`
}

type registry struct {
	Hash       string
	Operations map[string]operation
	LoadedAt   time.Time
}

type Gateway struct {
	config   Config
	client   *http.Client
	mu       sync.RWMutex
	current  *registry
	versions map[string]*registry
}

func New(config Config) (*Gateway, error) {
	parsed, err := url.Parse(config.CoreURL)
	if err != nil || parsed.Scheme != "http" && parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil {
		return nil, fmt.Errorf("platform gateway Core URL is invalid")
	}
	if config.OpenAPILoader == nil {
		return nil, fmt.Errorf("platform gateway OpenAPI loader is required")
	}
	if config.Timeout <= 0 {
		config.Timeout = 15 * time.Second
	}
	if config.MaxResponse <= 0 {
		config.MaxResponse = 1024 * 1024
	}
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12, InsecureSkipVerify: !config.CoreTLSVerify} //nolint:gosec -- deployment-controlled compatibility option
	if config.CACert != "" {
		content, readErr := os.ReadFile(config.CACert)
		if readErr != nil {
			return nil, fmt.Errorf("read platform gateway CA certificate: %w", readErr)
		}
		roots, loadErr := x509.SystemCertPool()
		if loadErr != nil || roots == nil {
			roots = x509.NewCertPool()
		}
		if !roots.AppendCertsFromPEM(content) {
			return nil, fmt.Errorf("platform gateway CA certificate is invalid")
		}
		tlsConfig.RootCAs = roots
	}
	if config.ClientCert != "" || config.ClientKey != "" {
		if config.ClientCert == "" || config.ClientKey == "" {
			return nil, fmt.Errorf("platform gateway client certificate configuration is incomplete")
		}
		certificate, loadErr := tls.LoadX509KeyPair(config.ClientCert, config.ClientKey)
		if loadErr != nil {
			return nil, fmt.Errorf("load platform gateway client certificate: %w", loadErr)
		}
		tlsConfig.Certificates = []tls.Certificate{certificate}
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.TLSClientConfig = tlsConfig
	client := &http.Client{Transport: transport, Timeout: config.Timeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	return &Gateway{config: config, client: client, versions: make(map[string]*registry)}, nil
}

func (g *Gateway) Registrations(_ context.Context, principal domain.Principal, profile string) ([]domain.Registration, error) {
	if !profileEnabled(profile, principal) {
		return nil, fmt.Errorf("platform profile is not available")
	}
	g.mu.RLock()
	registry := g.current
	g.mu.RUnlock()
	if registry == nil {
		return nil, fmt.Errorf("Core capability registry is still initializing")
	}
	return registrationsFor(registry, profile), nil
}

func registrationsFor(registry *registry, profile string) []domain.Registration {
	searchSchema := json.RawMessage(`{"type":"object","properties":{"query":{"type":"string","minLength":1,"maxLength":512},"progress":{"type":"string","maxLength":512},"action":{"type":"string","maxLength":128}},"required":["query"],"additionalProperties":false}`)
	callSchema := json.RawMessage(`{"type":"object","properties":{"operation_id":{"type":"string","minLength":1,"maxLength":256},"progress":{"type":"string","maxLength":512},"action":{"type":"string","maxLength":128},"path_params":{"type":"object"},"query_params":{"type":"object"},"body":{"oneOf":[{"type":"object"},{"type":"array"}]}},"required":["operation_id"],"additionalProperties":false}`)
	readAnnotations, _ := json.Marshal(domain.ToolAnnotations{ReadOnly: true, Idempotent: true})
	writeAnnotations, _ := json.Marshal(domain.ToolAnnotations{OpenWorld: true})
	definitions := []domain.Registration{
		{ID: "platform-gateway:search:" + registry.Hash, BindingKind: "service", ExecutionBindingID: "platform-gateway:" + registry.Hash, ClientKey: searchTool, Name: searchTool, Description: "Immediately search JumpServer Core operations protected by RBAC. Static permissions are checked before discovery; Core checks dynamic permissions against the actual request. Results include operation-specific guidance and server-enforced create-reuse policies. Use read operations to resolve related IDs and defaults before a write; do not ask the user for data that Core can supply.", InputSchema: searchSchema, DefinitionVersion: registry.Hash, Namespace: profile, Risk: "read", AnnotationsJSON: readAnnotations, State: "active"},
		{ID: "platform-gateway:call:" + registry.Hash, BindingKind: "service", ExecutionBindingID: "platform-gateway:" + registry.Hash, ClientKey: callTool, Name: callTool, Description: "Call one RBAC-protected Core operation selected from search results. Server-declared create-reuse policies are enforced automatically and may return an existing equivalent resource without a mutation. Never put passwords or other secrets in tool arguments; supported write-only fields are collected in the trusted approval UI. A remaining write call automatically opens that UI, so do not ask for chat confirmation.", InputSchema: callSchema, DefinitionVersion: registry.Hash, Namespace: profile, Risk: "dangerous", RequiresConfirmation: true, AnnotationsJSON: writeAnnotations, State: "active"},
	}
	for index := range definitions {
		definitions[index].DefinitionDigest, _ = domain.HashValue(map[string]any{"name": definitions[index].Name, "description": definitions[index].Description, "schema": definitions[index].InputSchema, "version": registry.Hash})
	}
	return definitions
}

func (g *Gateway) Prepare(ctx context.Context, request ports.CapabilityRequest) (ports.CapabilityPolicy, error) {
	registry, operation, arguments, err := g.resolveRequest(ctx, request)
	if err != nil {
		return ports.CapabilityPolicy{}, err
	}
	if request.Registration.Name == searchTool {
		return ports.CapabilityPolicy{Risk: "read"}, nil
	}
	if operation == nil {
		return ports.CapabilityPolicy{}, fmt.Errorf("operation is unavailable")
	}
	secretFields := secretInputFields(*operation)
	path, _, body, err := buildRequest(withoutSecretInputFields(*operation, secretFields), arguments)
	if err != nil {
		return ports.CapabilityPolicy{}, &ports.InvalidCapabilityArgumentsError{Detail: boundedString(err.Error(), 512)}
	}
	passwordRequired, err := accountPasswordRequired(operation.ID, body)
	if err != nil {
		return ports.CapabilityPolicy{}, &ports.InvalidCapabilityArgumentsError{Detail: err.Error()}
	}
	risk := methodRisk(operation.Method)
	previewValue := map[string]any{"operation_id": operation.ID, "method": operation.Method, "path": path, "summary": operation.Summary, "arguments": sanitize(arguments, 0)}
	if passwordRequired {
		previewValue["account_password_required"] = true
	}
	if len(secretFields) > 0 {
		previewValue["secret_input_fields"] = secretFields
	}
	resource, reused, reuseErr := g.findReusable(ctx, registry, *operation, request, body)
	if reuseErr != nil {
		return ports.CapabilityPolicy{}, reuseErr
	}
	if operation.CreateReuse != nil {
		previewValue["create_reuse"] = map[string]any{"lookup_operation_id": operation.CreateReuse.LookupOperationID, "matched": reused}
	}
	if reused {
		risk = "read"
		previewValue["create_reuse"].(map[string]any)["resource"] = resource
	}
	preview, _ := json.Marshal(previewValue)
	return ports.CapabilityPolicy{Risk: risk, RequiresConfirmation: risk != "read", Preview: preview}, nil
}

func (g *Gateway) Execute(ctx context.Context, request ports.CapabilityRequest) (ports.CapabilityResult, error) {
	registry, operation, arguments, err := g.resolveRequest(ctx, request)
	if err != nil {
		return ports.CapabilityResult{}, err
	}
	if request.Registration.Name == searchTool {
		query, _ := arguments["query"].(string)
		candidates := searchOperations(registry, request.Profile, request.Principal, query, 5, g.config.AllowedMethods)
		encoded, _ := json.Marshal(map[string]any{"operations": candidates, "registry_version": registry.Hash})
		return ports.CapabilityResult{Status: "success", Result: encoded, ExecutorAuditReference: "platform-search:" + uuid.NewString()}, nil
	}
	secretFields := secretInputFields(*operation)
	if operation.CreateReuse != nil {
		_, _, lookupBody, lookupErr := buildRequest(withoutSecretInputFields(*operation, secretFields), arguments)
		if lookupErr != nil {
			return ports.CapabilityResult{}, &ports.InvalidCapabilityArgumentsError{Detail: boundedString(lookupErr.Error(), 512)}
		}
		if _, lookupErr = accountPasswordRequired(operation.ID, lookupBody); lookupErr != nil {
			return ports.CapabilityResult{}, lookupErr
		}
		resource, reused, reuseErr := g.findReusable(ctx, registry, *operation, request, lookupBody)
		if reuseErr != nil {
			return ports.CapabilityResult{}, reuseErr
		}
		if reused {
			return reusedResult(*operation, operation.CreateReuse, redactKnownSecrets(resource, request)), nil
		}
	}
	if err := applySecretInputs(arguments, secretFields, request.SecretInputs); err != nil {
		return ports.CapabilityResult{}, err
	}
	path, query, body, err := buildRequest(*operation, arguments)
	if err != nil {
		detail := boundedString(err.Error(), 512)
		if len(request.SecretInputs) > 0 {
			detail = "The approved secret or request fields did not satisfy the Core request schema."
		}
		encoded, _ := json.Marshal(map[string]any{"code": "invalid_operation_arguments", "message": "The operation arguments did not match the Core request schema.", "detail": detail})
		return ports.CapabilityResult{Status: "error", Error: encoded}, nil
	}
	passwordRequired, err := accountPasswordRequired(operation.ID, body)
	if err != nil {
		return ports.CapabilityResult{}, err
	}
	if passwordRequired {
		if request.AccountPassword == "" {
			return ports.CapabilityResult{}, fmt.Errorf("account password was not supplied by the approval UI")
		}
	} else if request.AccountPassword != "" {
		return ports.CapabilityResult{}, fmt.Errorf("account password is not accepted for this operation")
	}
	resource, reused, reuseErr := g.findReusable(ctx, registry, *operation, request, body)
	if reuseErr != nil {
		return ports.CapabilityResult{}, reuseErr
	}
	if reused {
		return reusedResult(*operation, operation.CreateReuse, redactKnownSecrets(resource, request)), nil
	}
	if passwordRequired {
		applyAccountPassword(body, request.AccountPassword)
	}
	// Registry search and reuse lookups do not issue a Core mutation.
	// Management operations use this same execution path and are never exempt.
	if requiresApproval(operation.Method) && strings.TrimSpace(request.ApprovalID) == "" {
		return ports.CapabilityResult{}, fmt.Errorf("Core write operation requires a non-empty approval ID")
	}
	var bodyReader io.Reader
	var bodyBytes []byte
	if requiresApproval(operation.Method) && len(operation.Body) > 0 {
		bodyBytes, err = json.Marshal(body)
		if err != nil {
			return ports.CapabilityResult{}, err
		}
		bodyReader = bytes.NewReader(bodyBytes)
	}
	endpoint := strings.TrimRight(g.config.CoreURL, "/") + path
	if query != "" {
		endpoint += "?" + query
	}
	httpRequest, err := http.NewRequestWithContext(ctx, operation.Method, endpoint, bodyReader)
	if err != nil {
		return ports.CapabilityResult{}, err
	}
	httpRequest.Header.Set("Accept", "application/json")
	httpRequest.Header.Set("X-JMS-ORG", request.Principal.OrganizationID)
	httpRequest.Header.Set("X-JMS-AI-Operation", operation.ID)
	if len(bodyBytes) > 0 {
		httpRequest.Header.Set("Content-Type", "application/json")
	}
	if err = identity.CoreCredentialsFromContext(ctx).Apply(httpRequest); err != nil {
		return ports.CapabilityResult{}, err
	}
	response, err := g.client.Do(httpRequest)
	if err != nil {
		return ports.CapabilityResult{}, err
	}
	defer response.Body.Close()
	content, err := io.ReadAll(io.LimitReader(response.Body, g.config.MaxResponse+1))
	if err != nil {
		return ports.CapabilityResult{}, err
	}
	if int64(len(content)) > g.config.MaxResponse {
		return ports.CapabilityResult{}, fmt.Errorf("Core response exceeds the configured limit")
	}
	var value any
	if len(content) > 0 && json.Unmarshal(content, &value) != nil {
		value = boundedString(string(content), 8192)
	}
	clean := redactKnownSecrets(sanitize(value, 0), request)
	ok := response.StatusCode >= 200 && response.StatusCode < 300
	result := map[string]any{"ok": ok, "status_code": response.StatusCode, "operation_id": operation.ID, "data": clean}
	encoded, _ := json.Marshal(result)
	if len(encoded) > domain.MaxToolResultBytes {
		encoded, _ = json.Marshal(map[string]any{"ok": result["ok"], "status_code": response.StatusCode, "operation_id": operation.ID, "data": map[string]any{"truncated": true, "preview": boundedString(string(encoded), 32*1024)}})
	}
	cards, _ := json.Marshal([]any{buildResultCard(*operation, response.StatusCode, clean)})
	if len(cards) > 64*1024 {
		cards, _ = json.Marshal([]any{map[string]any{"type": "detail", "title": operation.Summary, "source": map[string]any{"type": "core_api", "operation_id": operation.ID, "method": operation.Method, "path": operation.Path, "status_code": response.StatusCode}, "content": map[string]any{"truncated": true, "preview": boundedString(string(cards), 32*1024)}}})
	}
	capabilityResult := ports.CapabilityResult{Status: "success", Result: encoded, ResultCards: cards, ExecutorAuditReference: "core-api:" + uuid.NewString()}
	if !ok {
		capabilityResult.Status = "error"
		capabilityResult.Error, _ = json.Marshal(map[string]any{"code": "core_api_error", "message": "Core rejected the authorized operation.", "status_code": response.StatusCode, "operation_id": operation.ID, "data": clean})
	}
	return capabilityResult, nil
}

func (g *Gateway) findReusable(ctx context.Context, registry *registry, requested operation, request ports.CapabilityRequest, body any) (any, bool, error) {
	policy := requested.CreateReuse
	if policy == nil {
		return nil, false, nil
	}
	lookup, ok := registry.Operations[policy.LookupOperationID]
	if !ok || !operationAllowed(request.Profile, request.Principal, lookup, g.config.AllowedMethods) {
		return nil, false, fmt.Errorf("Core reuse lookup operation is not allowed")
	}
	queryParameters, err := reuseQueryParameters(policy, body)
	if err != nil {
		return nil, false, &ports.InvalidCapabilityArgumentsError{Detail: boundedString(err.Error(), 512)}
	}
	path, query, _, err := buildRequest(lookup, map[string]any{"query_params": queryParameters})
	if err != nil {
		return nil, false, fmt.Errorf("build Core reuse lookup: %w", err)
	}
	statusCode, value, err := g.executeCoreRead(ctx, request, lookup, path, query)
	if err != nil {
		return nil, false, err
	}
	if statusCode < 200 || statusCode >= 300 {
		return nil, false, fmt.Errorf("Core reuse lookup returned HTTP %d", statusCode)
	}
	var rows []any
	switch item := value.(type) {
	case map[string]any:
		rawRows, exists := item["results"]
		if !exists {
			return nil, false, fmt.Errorf("Core reuse lookup response is not a paginated list")
		}
		rows, ok = rawRows.([]any)
		if !ok {
			return nil, false, fmt.Errorf("Core reuse lookup results are invalid")
		}
	case []any:
		rows = item
	default:
		return nil, false, fmt.Errorf("Core reuse lookup response is invalid")
	}
	if len(rows) == 0 {
		return nil, false, nil
	}
	for _, row := range rows {
		if reusableRowMatches(policy, row, queryParameters) {
			return sanitize(row, 0), true, nil
		}
	}
	return nil, false, nil
}

func reusableRowMatches(policy *createReusePolicy, row any, expected map[string]any) bool {
	object, ok := row.(map[string]any)
	if !ok {
		return false
	}
	for _, field := range policy.Fields {
		actual, exists := object[field.BodyField]
		if !exists {
			return false
		}
		if field.Unwrap != "" {
			if wrapped, nested := actual.(map[string]any); nested {
				actual, exists = wrapped[field.Unwrap]
				if !exists {
					return false
				}
			}
		}
		actualText, actualErr := queryScalar(actual)
		expectedText, expectedErr := queryScalar(expected[field.QueryParameter])
		if actualErr != nil || expectedErr != nil || actualText != expectedText {
			return false
		}
	}
	return true
}

func reuseQueryParameters(policy *createReusePolicy, body any) (map[string]any, error) {
	object, ok := body.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("create-reuse policy requires an object request body")
	}
	query := make(map[string]any, len(policy.Fields))
	for _, field := range policy.Fields {
		value, exists := object[field.BodyField]
		if !exists {
			if !field.HasDefault {
				return nil, fmt.Errorf("create-reuse body field %q is missing", field.BodyField)
			}
			value = field.Default
		}
		if field.Unwrap != "" {
			if wrapped, nested := value.(map[string]any); nested {
				value, exists = wrapped[field.Unwrap]
				if !exists {
					return nil, fmt.Errorf("create-reuse body field %q has no %q value", field.BodyField, field.Unwrap)
				}
			}
		}
		if _, err := queryScalar(value); err != nil {
			return nil, fmt.Errorf("create-reuse body field %q is not scalar", field.BodyField)
		}
		query[field.QueryParameter] = value
	}
	return query, nil
}

func (g *Gateway) executeCoreRead(ctx context.Context, request ports.CapabilityRequest, operation operation, path, query string) (int, any, error) {
	endpoint := strings.TrimRight(g.config.CoreURL, "/") + path
	if query != "" {
		endpoint += "?" + query
	}
	httpRequest, err := http.NewRequestWithContext(ctx, operation.Method, endpoint, nil)
	if err != nil {
		return 0, nil, err
	}
	httpRequest.Header.Set("Accept", "application/json")
	httpRequest.Header.Set("X-JMS-ORG", request.Principal.OrganizationID)
	httpRequest.Header.Set("X-JMS-AI-Operation", operation.ID)
	if err = identity.CoreCredentialsFromContext(ctx).Apply(httpRequest); err != nil {
		return 0, nil, err
	}
	response, err := g.client.Do(httpRequest)
	if err != nil {
		return 0, nil, err
	}
	defer response.Body.Close()
	content, err := io.ReadAll(io.LimitReader(response.Body, g.config.MaxResponse+1))
	if err != nil {
		return 0, nil, err
	}
	if int64(len(content)) > g.config.MaxResponse {
		return 0, nil, fmt.Errorf("Core reuse lookup response exceeds the configured limit")
	}
	var value any
	if len(content) > 0 {
		decoder := json.NewDecoder(bytes.NewReader(content))
		decoder.UseNumber()
		if err = decoder.Decode(&value); err != nil {
			return 0, nil, fmt.Errorf("Core reuse lookup returned invalid JSON")
		}
	}
	return response.StatusCode, value, nil
}

func reusedResult(operation operation, policy *createReusePolicy, data any) ports.CapabilityResult {
	result := map[string]any{"ok": true, "status_code": http.StatusOK, "operation_id": operation.ID, "lookup_operation_id": policy.LookupOperationID, "reused": true, "data": data}
	encoded, _ := json.Marshal(result)
	if len(encoded) > domain.MaxToolResultBytes {
		result["data"] = map[string]any{"truncated": true, "preview": boundedString(string(encoded), 32*1024)}
		encoded, _ = json.Marshal(result)
	}
	card := buildResultCard(operation, http.StatusOK, data)
	if source, ok := card["source"].(map[string]any); ok {
		source["reused"] = true
		source["lookup_operation_id"] = policy.LookupOperationID
	}
	cards, _ := json.Marshal([]any{card})
	if len(cards) > 64*1024 {
		cards, _ = json.Marshal([]any{map[string]any{"type": "detail", "title": operation.Summary, "source": map[string]any{"type": "core_api", "operation_id": operation.ID, "lookup_operation_id": policy.LookupOperationID, "reused": true}, "content": map[string]any{"truncated": true, "preview": boundedString(string(cards), 32*1024)}}})
	}
	return ports.CapabilityResult{Status: "success", Result: encoded, ResultCards: cards, ExecutorAuditReference: "core-api-reuse:" + uuid.NewString()}
}

func buildResultCard(operation operation, status int, data any) map[string]any {
	rows := resultRows(data)
	kind := "detail"
	content := data
	if len(rows) > 0 {
		kind = "table"
		for _, marker := range []string{"session", "command", "audit", "login_log", "operate_log", "job_log"} {
			if strings.Contains(operation.ID, marker) {
				kind = "timeline"
				break
			}
		}
		preferred := []string(nil)
		if operation.ID == "assets_assets_list" || operation.ID == "assets_hosts_list" || operation.ID == "assets_nodes_assets_list" {
			preferred = []string{"name", "address", "platform", "accounts_amount", "is_active", "date_verified"}
		}
		content = tableContent(rows, preferred)
		if object, ok := data.(map[string]any); ok {
			if total, ok := object["count"].(json.Number); ok {
				content.(map[string]any)["total"] = total
			} else if total, ok := object["count"].(float64); ok {
				content.(map[string]any)["total"] = total
			}
		}
	} else if strings.Contains(operation.ID, "metric") || strings.Contains(operation.ID, "status") {
		kind = "metric"
	}
	title := operation.Summary
	if title == "" {
		title = operation.ID
	}
	if operation.ID == "assets_assets_list" || operation.ID == "assets_hosts_list" || operation.ID == "assets_nodes_assets_list" {
		title = "Assets"
	}
	return map[string]any{"type": kind, "title": title, "source": map[string]any{"type": "core_api", "operation_id": operation.ID, "method": operation.Method, "path": operation.Path, "status_code": status}, "content": content}
}

func resultRows(data any) []any {
	if object, ok := data.(map[string]any); ok {
		rows, _ := object["results"].([]any)
		return rows
	}
	rows, _ := data.([]any)
	return rows
}

func tableContent(rows []any, preferred []string) map[string]any {
	objects := make([]map[string]any, 0, min(len(rows), 20))
	for _, row := range rows {
		if object, ok := row.(map[string]any); ok {
			objects = append(objects, object)
			if len(objects) == 20 {
				break
			}
		}
	}
	columns := make([]string, 0, 10)
	seen := map[string]bool{}
	if len(preferred) > 0 {
		for _, key := range preferred {
			for _, row := range objects {
				if displayScalar(row[key]) != nil {
					columns = append(columns, key)
					break
				}
			}
		}
	} else {
		for _, row := range objects {
			for key, value := range row {
				if !seen[key] && displayScalar(value) != nil {
					seen[key] = true
					columns = append(columns, key)
					if len(columns) == 10 {
						break
					}
				}
			}
			if len(columns) == 10 {
				break
			}
		}
	}
	tableRows := make([]map[string]any, 0, len(objects))
	for index, row := range objects {
		item := make(map[string]any, len(columns)+1)
		for _, column := range columns {
			item[column] = displayScalar(row[column])
		}
		item["_key"] = firstValue(row["id"], row["pk"], index)
		// Keep the resource identifier separate from the display key, whose
		// fallback is only a row index and must never become a detail link.
		switch id := firstValue(row["id"], row["pk"]).(type) {
		case string:
			if strings.TrimSpace(id) != "" {
				item["_resource_id"] = id
			}
		case json.Number, float64:
			item["_resource_id"] = fmt.Sprint(id)
		}
		tableRows = append(tableRows, item)
	}
	result := map[string]any{"columns": columns, "rows": tableRows}
	if len(preferred) > 0 {
		result["variant"] = "assets"
	}
	return result
}

func displayScalar(value any) any {
	if object, ok := value.(map[string]any); ok {
		for _, key := range []string{"name", "label", "display_name", "value"} {
			if scalar(object[key]) {
				return object[key]
			}
		}
		return nil
	}
	if scalar(value) {
		return value
	}
	return nil
}

func scalar(value any) bool {
	switch value.(type) {
	case nil, string, bool, float64, json.Number:
		return true
	default:
		return false
	}
}

func firstValue(values ...any) any {
	for _, value := range values {
		if value != nil && value != "" {
			return value
		}
	}
	return nil
}

func (g *Gateway) Refresh(ctx context.Context) (map[string]any, error) {
	registry, err := g.load(ctx)
	if err != nil {
		return nil, err
	}
	return map[string]any{"registry_version": registry.Hash, "operations": len(registry.Operations), "loaded_at": registry.LoadedAt}, nil
}

func (g *Gateway) resolveRequest(ctx context.Context, request ports.CapabilityRequest) (*registry, *operation, map[string]any, error) {
	if !profileEnabled(request.Profile, request.Principal) {
		return nil, nil, nil, fmt.Errorf("platform profile is not available")
	}
	var arguments map[string]any
	decoder := json.NewDecoder(bytes.NewReader(request.Arguments))
	decoder.UseNumber()
	if decoder.Decode(&arguments) != nil || arguments == nil {
		return nil, nil, nil, fmt.Errorf("capability arguments are invalid")
	}
	if containsSensitive(arguments) {
		return nil, nil, nil, &ports.InvalidCapabilityArgumentsError{
			Detail: "Credential fields cannot be sent through the AI assistant. Create the host without embedded credentials, then use a separately authorized account creation operation and enter the password in its approval form.",
		}
	}
	version := strings.TrimPrefix(request.Registration.ExecutionBindingID, "platform-gateway:")
	registry, err := g.version(ctx, version)
	if err != nil {
		return nil, nil, nil, err
	}
	registrations := registrationsFor(registry, request.Profile)
	validDefinition := false
	for _, registration := range registrations {
		if registration.Name == request.Registration.Name &&
			registration.DefinitionDigest == request.Registration.DefinitionDigest &&
			registration.DefinitionVersion == request.Registration.DefinitionVersion &&
			registration.ExecutionBindingID == request.Registration.ExecutionBindingID {
			validDefinition = true
			break
		}
	}
	if !validDefinition {
		return nil, nil, nil, fmt.Errorf("capability definition is stale")
	}
	if request.Registration.Name == searchTool {
		return registry, nil, arguments, nil
	}
	if request.Registration.Name != callTool {
		return nil, nil, nil, fmt.Errorf("unknown service capability")
	}
	operationID, _ := arguments["operation_id"].(string)
	operation, ok := registry.Operations[operationID]
	if !ok || !operationAllowed(request.Profile, request.Principal, operation, g.config.AllowedMethods) {
		return nil, nil, nil, fmt.Errorf("Core operation is not allowed")
	}
	return registry, &operation, arguments, nil
}

func (g *Gateway) version(_ context.Context, hash string) (*registry, error) {
	g.mu.RLock()
	value := g.versions[hash]
	g.mu.RUnlock()
	if value == nil {
		return nil, fmt.Errorf("platform registry version is unavailable")
	}
	return value, nil
}

func (g *Gateway) load(ctx context.Context) (*registry, error) {
	schema, err := g.config.OpenAPILoader(ctx)
	if err != nil {
		return nil, err
	}
	content, err := json.Marshal(schema)
	if err != nil || len(content) > 32*1024*1024 {
		return nil, fmt.Errorf("Core OpenAPI response is invalid")
	}
	operations, err := parseOperations(schema)
	if err != nil {
		return nil, err
	}
	hash := domain.HashBytes(content)
	loaded := &registry{Hash: hash, Operations: operations, LoadedAt: time.Now().UTC()}
	g.mu.Lock()
	g.current, g.versions[hash] = loaded, loaded
	if len(g.versions) > 4 {
		oldestHash, oldestTime := "", loaded.LoadedAt
		for candidate, version := range g.versions {
			if candidate != hash && !version.LoadedAt.After(oldestTime) {
				oldestHash, oldestTime = candidate, version.LoadedAt
			}
		}
		if oldestHash != "" {
			delete(g.versions, oldestHash)
		}
	}
	g.mu.Unlock()
	return loaded, nil
}

func parseOperations(schema map[string]any) (map[string]operation, error) {
	paths, _ := schema["paths"].(map[string]any)
	result := make(map[string]operation)
	for path, rawPath := range paths {
		pathItem, _ := resolve(schema, rawPath, 0).(map[string]any)
		for method, rawOperation := range pathItem {
			upper := strings.ToUpper(method)
			if !map[string]bool{"GET": true, "HEAD": true, "POST": true, "PUT": true, "PATCH": true, "DELETE": true}[upper] {
				continue
			}
			item, _ := resolve(schema, rawOperation, 0).(map[string]any)
			if item == nil {
				continue
			}
			id, _ := item["operationId"].(string)
			if id == "" {
				id = strings.ToLower(upper) + "_" + regexp.MustCompile(`[^A-Za-z0-9]+`).ReplaceAllString(strings.Trim(path, "/"), "_")
			}
			requiredPermissions, staticPermissions := permissionMetadata(item["x-jms-required-permissions"])
			permissionDynamic, dynamicMetadata := item["x-jms-permission-dynamic"].(bool)
			rbacProtected, _ := item["x-jms-rbac-protected"].(bool)
			safeSensitivePath, _ := item["x-jms-safe-sensitive-path"].(bool)
			createReuse, reuseErr := parseCreateReusePolicy(item["x-jms-create-reuse"])
			if reuseErr != nil {
				return nil, fmt.Errorf("Core OpenAPI operation %s has invalid create-reuse metadata: %w", id, reuseErr)
			}
			entry := operation{
				ID: id, Method: upper, Path: path, Summary: stringValue(item["summary"]), Description: stringValue(item["description"]), Tags: stringSlice(item["tags"]),
				RequiredPermissions: requiredPermissions,
				PermissionDynamic:   !staticPermissions || !dynamicMetadata || permissionDynamic,
				RBACProtected:       rbacProtected,
				SafeSensitivePath:   safeSensitivePath,
				Guidance:            stringValue(item["x-jms-ai-guidance"]),
				CreateReuse:         createReuse,
			}
			parameters := append(anySlice(pathItem["parameters"]), anySlice(item["parameters"])...)
			for _, rawParameter := range parameters {
				value, _ := resolve(schema, rawParameter, 0).(map[string]any)
				location := stringValue(value["in"])
				name := stringValue(value["name"])
				if name == "" {
					continue
				}
				parameterSchema, _ := resolve(schema, value["schema"], 0).(map[string]any)
				style := stringValue(value["style"])
				if style == "" {
					style = "form"
				}
				parameter := parameter{Name: name, Required: boolValue(value["required"]), Style: style, Explode: boolDefault(value["explode"], style == "form"), Schema: parameterSchema}
				if location == "path" {
					entry.PathParams = append(entry.PathParams, parameter)
				} else if location == "query" {
					entry.QueryParams = append(entry.QueryParams, parameter)
				}
			}
			requestBody, _ := resolve(schema, item["requestBody"], 0).(map[string]any)
			content, _ := requestBody["content"].(map[string]any)
			media, _ := content["application/json"].(map[string]any)
			entry.Body, _ = resolve(schema, media["schema"], 0).(map[string]any)
			entry.Body = requestSchema(entry.Body)
			entry.BodyRequired = boolValue(requestBody["required"])
			result[id] = entry
		}
	}
	if len(result) == 0 {
		return nil, fmt.Errorf("Core OpenAPI contains no supported operations")
	}
	if err := validateCreateReusePolicies(result); err != nil {
		return nil, err
	}
	return result, nil
}

func parseCreateReusePolicy(value any) (*createReusePolicy, error) {
	if value == nil {
		return nil, nil
	}
	metadata, ok := value.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("metadata must be an object")
	}
	lookupOperationID := strings.TrimSpace(stringValue(metadata["lookup_operation_id"]))
	rawFields, fieldsOK := metadata["fields"].([]any)
	if lookupOperationID == "" || !fieldsOK || len(rawFields) == 0 || len(rawFields) > 16 {
		return nil, fmt.Errorf("lookup_operation_id and 1-16 fields are required")
	}
	policy := &createReusePolicy{LookupOperationID: lookupOperationID, Fields: make([]reuseField, 0, len(rawFields))}
	seenBody, seenQuery := map[string]bool{}, map[string]bool{}
	for _, rawField := range rawFields {
		metadataField, ok := rawField.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("field mapping must be an object")
		}
		field := reuseField{
			BodyField:      strings.TrimSpace(stringValue(metadataField["body_field"])),
			QueryParameter: strings.TrimSpace(stringValue(metadataField["query_parameter"])),
			Unwrap:         strings.TrimSpace(stringValue(metadataField["unwrap"])),
		}
		field.Default, field.HasDefault = metadataField["default"]
		if field.BodyField == "" || field.QueryParameter == "" || seenBody[field.BodyField] || seenQuery[field.QueryParameter] {
			return nil, fmt.Errorf("field mappings must use unique non-empty body and query names")
		}
		if field.Unwrap != "" && field.Unwrap != "value" {
			return nil, fmt.Errorf("unsupported field unwrap %q", field.Unwrap)
		}
		if field.HasDefault {
			if _, err := queryScalar(field.Default); err != nil {
				return nil, fmt.Errorf("default for %q must be scalar", field.BodyField)
			}
		}
		seenBody[field.BodyField], seenQuery[field.QueryParameter] = true, true
		policy.Fields = append(policy.Fields, field)
	}
	return policy, nil
}

func validateCreateReusePolicies(operations map[string]operation) error {
	for _, operation := range operations {
		policy := operation.CreateReuse
		if policy == nil {
			continue
		}
		if operation.Method != http.MethodPost {
			return fmt.Errorf("Core OpenAPI create-reuse operation %s must use POST", operation.ID)
		}
		lookup, ok := operations[policy.LookupOperationID]
		if !ok || lookup.Method != http.MethodGet {
			return fmt.Errorf("Core OpenAPI create-reuse lookup %s must be a GET operation", policy.LookupOperationID)
		}
		bodyProperties, _ := operation.Body["properties"].(map[string]any)
		lookupParameters := make(map[string]bool, len(lookup.QueryParams))
		for _, parameter := range lookup.QueryParams {
			lookupParameters[parameter.Name] = true
		}
		secretFields := make(map[string]bool)
		for _, field := range secretInputFields(operation) {
			secretFields[field.Name] = true
		}
		for _, field := range policy.Fields {
			if secretFields[field.BodyField] {
				return fmt.Errorf("Core create-reuse policy cannot query a secret field")
			}
			if _, ok := bodyProperties[field.BodyField]; !ok {
				return fmt.Errorf("Core OpenAPI create-reuse body field %s.%s does not exist", operation.ID, field.BodyField)
			}
			if !lookupParameters[field.QueryParameter] {
				return fmt.Errorf("Core OpenAPI create-reuse query parameter %s.%s does not exist", policy.LookupOperationID, field.QueryParameter)
			}
		}
	}
	return nil
}

func permissionMetadata(value any) ([]string, bool) {
	items, ok := value.([]any)
	if !ok {
		return nil, false
	}
	permissions := make([]string, 0, len(items))
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		permission, ok := item.(string)
		permission = strings.TrimSpace(permission)
		if !ok || permission == "" {
			return nil, false
		}
		if _, exists := seen[permission]; exists {
			continue
		}
		seen[permission] = struct{}{}
		permissions = append(permissions, permission)
	}
	sort.Strings(permissions)
	return permissions, true
}

// requestSchema converts the OpenAPI request view into strict JSON Schema.
// Shared DRF schemas contain response-only required fields and nullable
// annotations that must not reject otherwise valid request bodies.
func requestSchema(schema map[string]any) map[string]any {
	value, _ := normalizeRequestSchema(schema).(map[string]any)
	return value
}

func normalizeRequestSchema(value any) any {
	switch item := value.(type) {
	case map[string]any:
		removed := map[string]bool{}
		if properties, ok := item["properties"].(map[string]any); ok {
			for name, raw := range properties {
				property, _ := raw.(map[string]any)
				if readOnly, _ := property["readOnly"].(bool); readOnly {
					removed[name] = true
				}
			}
		}
		result := make(map[string]any, len(item))
		for key, child := range item {
			if (key == "description" || key == "title" || key == "$comment") && child == nil {
				continue
			}
			switch key {
			case "nullable":
				continue
			case "properties":
				properties, _ := child.(map[string]any)
				normalized := make(map[string]any, len(properties)-len(removed))
				for name, property := range properties {
					if !removed[name] {
						normalized[name] = normalizeRequestSchema(property)
					}
				}
				result[key] = normalized
			case "required":
				required, _ := child.([]any)
				normalized := make([]any, 0, len(required))
				for _, name := range required {
					text, _ := name.(string)
					if !removed[text] {
						normalized = append(normalized, name)
					}
				}
				if len(normalized) > 0 {
					result[key] = normalized
				}
			default:
				result[key] = normalizeRequestSchema(child)
			}
		}
		if nullable, _ := item["nullable"].(bool); nullable {
			if schemaType, ok := result["type"].(string); ok {
				result["type"] = []any{schemaType, "null"}
			}
		}
		return result
	case []any:
		result := make([]any, len(item))
		for index, child := range item {
			result[index] = normalizeRequestSchema(child)
		}
		return result
	default:
		return value
	}
}

func resolve(root map[string]any, value any, _ int) any {
	return resolveRefs(root, value, map[string]bool{}, 0)
}

func resolveRefs(root map[string]any, value any, seen map[string]bool, depth int) any {
	if depth > 128 {
		return map[string]any{}
	}
	switch item := value.(type) {
	case map[string]any:
		if reference, _ := item["$ref"].(string); strings.HasPrefix(reference, "#/") {
			siblings := make(map[string]any, len(item)-1)
			for key, child := range item {
				if key != "$ref" {
					siblings[key] = child
				}
			}
			if seen[reference] {
				return resolveRefs(root, siblings, seen, depth+1)
			}
			target, ok := resolveReference(root, reference)
			if !ok {
				return resolveRefs(root, siblings, seen, depth+1)
			}
			if targetObject, ok := target.(map[string]any); ok {
				merged := make(map[string]any, len(targetObject)+len(siblings))
				for key, child := range targetObject {
					merged[key] = child
				}
				for key, child := range siblings {
					merged[key] = child
				}
				target = merged
			}
			nextSeen := make(map[string]bool, len(seen)+1)
			for key, value := range seen {
				nextSeen[key] = value
			}
			nextSeen[reference] = true
			return resolveRefs(root, target, nextSeen, depth+1)
		}
		result := make(map[string]any, len(item))
		for key, child := range item {
			result[key] = resolveRefs(root, child, seen, depth+1)
		}
		return result
	case []any:
		result := make([]any, len(item))
		for index, child := range item {
			result[index] = resolveRefs(root, child, seen, depth+1)
		}
		return result
	default:
		return value
	}
}

func resolveReference(root map[string]any, reference string) (any, bool) {
	var current any = root
	for _, part := range strings.Split(strings.TrimPrefix(reference, "#/"), "/") {
		object, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		key := strings.ReplaceAll(strings.ReplaceAll(part, "~1", "/"), "~0", "~")
		current, ok = object[key]
		if !ok {
			return nil, false
		}
	}
	return current, true
}

func buildRequest(operation operation, arguments map[string]any) (string, string, any, error) {
	pathValues, _ := arguments["path_params"].(map[string]any)
	if pathValues == nil {
		pathValues = map[string]any{}
	}
	queryValues, _ := arguments["query_params"].(map[string]any)
	if queryValues == nil {
		queryValues = map[string]any{}
	}
	path := operation.Path
	allowedPath := map[string]parameter{}
	for _, parameter := range operation.PathParams {
		allowedPath[parameter.Name] = parameter
		value, ok := pathValues[parameter.Name]
		if parameter.Required && !ok {
			return "", "", nil, fmt.Errorf("required path parameter is missing")
		}
		if ok {
			if err := validateValue(parameter.Schema, value); err != nil {
				return "", "", nil, err
			}
			path = strings.ReplaceAll(path, "{"+parameter.Name+"}", url.PathEscape(fmt.Sprint(value)))
		}
	}
	for name := range pathValues {
		if _, ok := allowedPath[name]; !ok {
			return "", "", nil, fmt.Errorf("unknown path parameter")
		}
	}
	if strings.ContainsAny(path, "{}") || !strings.HasPrefix(path, "/api/v1/") {
		return "", "", nil, fmt.Errorf("Core path is invalid")
	}
	allowedQuery := map[string]parameter{}
	for _, parameter := range operation.QueryParams {
		allowedQuery[parameter.Name] = parameter
		if parameter.Required {
			if _, ok := queryValues[parameter.Name]; !ok {
				return "", "", nil, fmt.Errorf("required query parameter is missing")
			}
		}
	}
	query := url.Values{}
	for name, value := range queryValues {
		parameter, ok := allowedQuery[name]
		if !ok {
			return "", "", nil, fmt.Errorf("unknown query parameter")
		}
		if err := validateValue(parameter.Schema, value); err != nil {
			return "", "", nil, err
		}
		switch item := value.(type) {
		case []any:
			values := make([]string, 0, len(item))
			for _, child := range item {
				encoded, encodeErr := queryScalar(child)
				if encodeErr != nil {
					return "", "", nil, encodeErr
				}
				values = append(values, encoded)
			}
			switch {
			case parameter.Style == "form" && parameter.Explode:
				for _, encoded := range values {
					query.Add(name, encoded)
				}
			case parameter.Style == "form":
				query.Set(name, strings.Join(values, ","))
			case parameter.Style == "spaceDelimited":
				query.Set(name, strings.Join(values, " "))
			case parameter.Style == "pipeDelimited":
				query.Set(name, strings.Join(values, "|"))
			default:
				return "", "", nil, fmt.Errorf("unsupported query parameter style")
			}
		case map[string]any:
			keys := make([]string, 0, len(item))
			for key := range item {
				keys = append(keys, key)
			}
			sort.Strings(keys)
			flattened := make([]string, 0, len(keys)*2)
			for _, key := range keys {
				encoded, encodeErr := queryScalar(item[key])
				if encodeErr != nil {
					return "", "", nil, encodeErr
				}
				switch {
				case parameter.Style == "deepObject":
					query.Set(name+"["+key+"]", encoded)
				case parameter.Style == "form" && parameter.Explode:
					query.Set(key, encoded)
				case parameter.Style == "form":
					flattened = append(flattened, key, encoded)
				default:
					return "", "", nil, fmt.Errorf("unsupported query parameter style")
				}
			}
			if parameter.Style == "form" && !parameter.Explode {
				query.Set(name, strings.Join(flattened, ","))
			}
		default:
			encoded, encodeErr := queryScalar(item)
			if encodeErr != nil {
				return "", "", nil, encodeErr
			}
			query.Set(name, encoded)
		}
	}
	body, hasBody := arguments["body"]
	if !hasBody {
		body = map[string]any{}
	}
	if operation.BodyRequired && !hasBody {
		return "", "", nil, fmt.Errorf("request body is required")
	}
	if len(operation.Body) > 0 {
		if err := validateValue(operation.Body, body); err != nil {
			return "", "", nil, err
		}
	} else if hasBody && body != nil && !emptyObject(body) {
		return "", "", nil, fmt.Errorf("operation does not accept a body")
	}
	return path, query.Encode(), body, nil
}

func queryScalar(value any) (string, error) {
	switch item := value.(type) {
	case nil:
		return "", nil
	case bool:
		if item {
			return "true", nil
		}
		return "false", nil
	case string, json.Number, float64:
		return fmt.Sprint(item), nil
	default:
		return "", fmt.Errorf("query parameter value must be scalar")
	}
}

func emptyObject(value any) bool {
	object, ok := value.(map[string]any)
	return ok && len(object) == 0
}

var approvalSecretFields = set(
	"password", "old_password", "new_password", "confirm_password",
	"secret", "old_secret", "new_secret", "passphrase", "private_key",
	"ssh_key", "token", "api_key", "access_key", "zip_encrypt_password",
)

func secretInputFields(operation operation) []secretInputField {
	if !requiresApproval(operation.Method) || operation.ID == "accounts_accounts_create" {
		return nil
	}
	properties, _ := operation.Body["properties"].(map[string]any)
	required := make(map[string]bool)
	for _, value := range anySlice(operation.Body["required"]) {
		if name, ok := value.(string); ok {
			required[name] = true
		}
	}
	fields := make([]secretInputField, 0)
	for name, raw := range properties {
		property, _ := raw.(map[string]any)
		if property == nil || !approvalSecretFields[normalizeKey(name)] &&
			!(sensitiveKey(name) && boolValue(property["writeOnly"])) && property["format"] != "password" {
			continue
		}
		if !schemaAcceptsString(property["type"]) {
			continue
		}
		fields = append(fields, secretInputField{Name: name, Required: required[name]})
	}
	sort.Slice(fields, func(i, j int) bool { return fields[i].Name < fields[j].Name })
	return fields
}

func schemaAcceptsString(value any) bool {
	if value == "string" {
		return true
	}
	for _, choice := range anySlice(value) {
		if choice == "string" {
			return true
		}
	}
	return false
}

func withoutSecretInputFields(operation operation, fields []secretInputField) operation {
	if len(fields) == 0 {
		return operation
	}
	copyOfBody := make(map[string]any, len(operation.Body))
	for name, value := range operation.Body {
		copyOfBody[name] = value
	}
	properties, _ := operation.Body["properties"].(map[string]any)
	copyOfProperties := make(map[string]any, len(properties))
	for name, value := range properties {
		copyOfProperties[name] = value
	}
	excluded := make(map[string]bool, len(fields))
	for _, field := range fields {
		delete(copyOfProperties, field.Name)
		excluded[field.Name] = true
	}
	copyOfBody["properties"] = copyOfProperties
	required := make([]any, 0)
	for _, raw := range anySlice(operation.Body["required"]) {
		name, _ := raw.(string)
		if !excluded[name] {
			required = append(required, raw)
		}
	}
	copyOfBody["required"] = required
	operation.Body = copyOfBody
	operation.BodyRequired = false
	return operation
}

func applySecretInputs(arguments map[string]any, fields []secretInputField, values map[string]string) error {
	if len(fields) == 0 {
		if len(values) > 0 {
			return fmt.Errorf("this operation does not accept approval secret inputs")
		}
		return nil
	}
	allowed := make(map[string]secretInputField, len(fields))
	for _, field := range fields {
		allowed[field.Name] = field
	}
	for name, value := range values {
		if _, ok := allowed[name]; !ok || len(value) > 40960 {
			return fmt.Errorf("approval secret input is invalid")
		}
	}
	body, _ := arguments["body"].(map[string]any)
	if body == nil {
		body = make(map[string]any)
		arguments["body"] = body
	}
	for _, field := range fields {
		value := values[field.Name]
		if field.Required && value == "" {
			return fmt.Errorf("a required secret must be entered in the approval form")
		}
		if value != "" {
			body[field.Name] = value
		}
	}
	return nil
}

// The AI supplies account metadata. The password is entered separately in the
// approval UI and added only to the outbound account-creation request.
func accountPasswordRequired(operationID string, body any) (bool, error) {
	if operationID == "assets_hosts_create" {
		if host, ok := body.(map[string]any); ok {
			if _, present := host["accounts"]; present {
				return false, fmt.Errorf("create the host without embedded accounts, then create its accounts separately")
			}
		}
	}
	if operationID != "accounts_accounts_create" {
		return false, nil
	}
	account, ok := body.(map[string]any)
	if !ok {
		return false, fmt.Errorf("account details must be an object")
	}
	secretType := account["secret_type"]
	if choice, wrapped := secretType.(map[string]any); wrapped {
		secretType = choice["value"]
	}
	if secretType != nil && secretType != "password" {
		return false, fmt.Errorf("the AI assistant can create password accounts only")
	}
	allowedFields := map[string]bool{
		"asset": true, "username": true, "name": true,
		"secret_type": true, "privileged": true, "is_active": true,
		"comment": true,
	}
	for field := range account {
		if !allowedFields[field] {
			return false, fmt.Errorf("this account field is not supported by the AI assistant")
		}
	}
	return true, nil
}

func applyAccountPassword(body any, password string) {
	body.(map[string]any)["secret"] = password
}

func validateValue(schema map[string]any, value any) error {
	if len(schema) == 0 {
		return nil
	}
	raw, _ := json.Marshal(schema)
	compiler := jsonschema.NewCompiler()
	document, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		return err
	}
	if err = compiler.AddResource("schema.json", document); err != nil {
		return err
	}
	compiled, err := compiler.Compile("schema.json")
	if err != nil {
		return err
	}
	return compiled.Validate(value)
}

func methodRisk(method string) string {
	if !requiresApproval(method) {
		return "read"
	}
	if method == http.MethodDelete {
		return "dangerous"
	}
	return "write"
}

func requiresApproval(method string) bool {
	return method != http.MethodGet && method != http.MethodHead
}

var scopes = map[string]map[string]bool{
	"platform.asset":         set("assets_assets_list", "assets_assets_retrieve", "assets_categories_list", "assets_hosts_list", "assets_hosts_retrieve", "assets_nodes_assets_list", "assets_nodes_list", "assets_nodes_retrieve", "assets_platforms_list", "assets_platforms_retrieve", "assets_protocols_list"),
	"platform.session_audit": set("audits_activities_list", "audits_login_logs_list", "audits_login_logs_retrieve", "audits_my_login_logs_list", "audits_my_login_logs_retrieve", "audits_operate_logs_list", "audits_operate_logs_retrieve", "audits_service_access_logs_list", "audits_service_access_logs_retrieve", "audits_tickets_list", "audits_tickets_retrieve", "terminal_commands_list", "terminal_commands_retrieve", "terminal_sessions_list", "terminal_sessions_retrieve", "terminal_tasks_list", "terminal_tasks_retrieve"),
	"platform.ops":           set("audits_job_logs_list", "audits_job_logs_retrieve", "audits_jobs_list", "audits_jobs_retrieve", "ops_jobs_list", "ops_jobs_retrieve", "ops_tasks_list", "ops_tasks_retrieve", "terminal_components_metrics_retrieve", "terminal_terminals_list", "terminal_terminals_retrieve"),
}

func set(values ...string) map[string]bool {
	result := make(map[string]bool, len(values))
	for _, value := range values {
		result[value] = true
	}
	return result
}
func profileEnabled(profile string, principal domain.Principal) bool {
	return profile == "general" || profile == "platform.management" && (principal.IsSuperuser || principal.IsOrgAdmin) || scopes[profile] != nil
}
func operationAllowed(profile string, principal domain.Principal, operation operation, methods map[string]bool) bool {
	if !methods[operation.Method] || sensitivePath(operation.Path) && !operation.SafeSensitivePath {
		return false
	}
	if profile == "general" {
		if !operation.RBACProtected {
			return false
		}
		// Core determines dynamic permissions from the actual target and body,
		// and checks them again using the logged-in user's credentials.
		return operation.PermissionDynamic || hasAllPermissions(principal, operation.RequiredPermissions)
	}
	if operation.PermissionDynamic || !hasAllPermissions(principal, operation.RequiredPermissions) {
		return false
	}
	if profile == "platform.management" {
		return true
	}
	return scopes[profile][operation.ID]
}

func hasAllPermissions(principal domain.Principal, required []string) bool {
	if principal.IsSuperuser {
		return true
	}
	available := make(map[string]struct{}, len(principal.Permissions))
	for _, permission := range principal.Permissions {
		available[permission] = struct{}{}
	}
	for _, permission := range required {
		if _, ok := available[permission]; !ok {
			return false
		}
	}
	return true
}
func sensitivePath(path string) bool {
	lowered := strings.ToLower(path)
	for _, marker := range []string{"password", "secret", "private-key", "private_key", "access-key", "access_key", "token", "credential", "backup"} {
		if strings.Contains(lowered, marker) {
			return true
		}
	}
	return strings.Contains(lowered, "chat-ai")
}

func searchOperations(registry *registry, profile string, principal domain.Principal, query string, limit int, methods map[string]bool) []map[string]any {
	tokens := tokenize(query)
	type scored struct {
		score     int
		operation operation
	}
	ranked := []scored{}
	for _, operation := range registry.Operations {
		if !operationAllowed(profile, principal, operation, methods) {
			continue
		}
		haystack := strings.ToLower(strings.Join([]string{operation.ID, operation.Summary, operation.Description, operation.Guidance, operation.Path, operation.Method, strings.Join(operation.Tags, " ")}, " "))
		score := 0
		for _, token := range tokens {
			if strings.Contains(haystack, token) {
				score++
			}
		}
		if score > 0 {
			ranked = append(ranked, scored{score: score, operation: operation})
		}
	}
	sort.Slice(ranked, func(i, j int) bool {
		if ranked[i].score == ranked[j].score {
			return ranked[i].operation.ID < ranked[j].operation.ID
		}
		return ranked[i].score > ranked[j].score
	})
	if len(ranked) > limit {
		ranked = ranked[:limit]
	}
	result := make([]map[string]any, 0, len(ranked))
	for _, item := range ranked {
		operation := item.operation
		candidate := map[string]any{"operation_id": operation.ID, "method": operation.Method, "path": operation.Path, "summary": operation.Summary, "description": boundedString(operation.Description, 500), "tags": operation.Tags, "risk_level": methodRisk(operation.Method), "requires_approval": requiresApproval(operation.Method), "path_parameters": compactParameters(operation.PathParams), "query_parameters": compactParameters(operation.QueryParams), "request_body_schema": compactSchema(operation.Body, 0)}
		if operation.PermissionDynamic {
			candidate["permission_check"] = "core_at_execution"
		}
		if operation.Guidance != "" {
			candidate["guidance"] = boundedString(operation.Guidance, 1000)
		}
		if operation.CreateReuse != nil {
			candidate["create_reuse"] = compactCreateReusePolicy(operation.CreateReuse)
		}
		result = append(result, candidate)
	}
	return result
}

func compactCreateReusePolicy(policy *createReusePolicy) map[string]any {
	fields := make([]map[string]any, 0, len(policy.Fields))
	for _, field := range policy.Fields {
		item := map[string]any{"body_field": field.BodyField, "query_parameter": field.QueryParameter}
		if field.Unwrap != "" {
			item["unwrap"] = field.Unwrap
		}
		if field.HasDefault {
			item["default"] = field.Default
		}
		fields = append(fields, item)
	}
	return map[string]any{"mode": "reuse_if_equivalent", "lookup_operation_id": policy.LookupOperationID, "fields": fields, "enforced_by_gateway": true}
}

func compactParameters(parameters []parameter) []map[string]any {
	result := make([]map[string]any, 0, len(parameters))
	for _, parameter := range parameters {
		result = append(result, map[string]any{
			"name":     parameter.Name,
			"required": parameter.Required,
			"style":    parameter.Style,
			"explode":  parameter.Explode,
			"schema":   compactSchema(parameter.Schema, 0),
		})
	}
	return result
}

func compactSchema(value any, depth int) any {
	if depth > 5 {
		return map[string]any{}
	}
	item, ok := value.(map[string]any)
	if !ok {
		return value
	}
	result := make(map[string]any)
	for _, key := range []string{"type", "format", "enum", "nullable", "default", "minimum", "maximum", "minLength", "maxLength", "pattern", "minItems", "maxItems", "additionalProperties"} {
		if child, exists := item[key]; exists {
			if object, nested := child.(map[string]any); nested {
				result[key] = compactSchema(object, depth+1)
			} else {
				result[key] = child
			}
		}
	}
	if description, ok := item["description"].(string); ok && description != "" {
		result["description"] = boundedString(description, 240)
	}
	keptProperties := map[string]bool{}
	if properties, ok := item["properties"].(map[string]any); ok {
		compact := make(map[string]any, len(properties))
		for name, child := range properties {
			if sensitiveKey(name) {
				continue
			}
			compact[name] = compactSchema(child, depth+1)
			keptProperties[name] = true
		}
		result["properties"] = compact
	}
	if required, ok := item["required"].([]any); ok {
		compact := make([]any, 0, len(required))
		for _, rawName := range required {
			name, _ := rawName.(string)
			if len(keptProperties) == 0 || keptProperties[name] {
				compact = append(compact, rawName)
			}
		}
		if len(compact) > 0 {
			result["required"] = compact
		}
	}
	if items, exists := item["items"]; exists {
		result["items"] = compactSchema(items, depth+1)
	}
	for _, key := range []string{"oneOf", "anyOf", "allOf"} {
		choices, ok := item[key].([]any)
		if !ok {
			continue
		}
		if len(choices) > 8 {
			choices = choices[:8]
		}
		compact := make([]any, len(choices))
		for index, choice := range choices {
			compact[index] = compactSchema(choice, depth+1)
		}
		result[key] = compact
	}
	return result
}

var wordPattern = regexp.MustCompile(`[\p{L}\p{N}_/-]+`)

func tokenize(value string) []string {
	normalized := strings.ToLower(value)
	aliases := []struct{ source, target string }{
		{"创建", "create"}, {"新建", "create"}, {"添加", "create"}, {"新增", "create"}, {"主机", "host"}, {"服务器", "host"}, {"机器", "host"},
		{"资产", "asset"}, {"平台", "platform"}, {"节点", "node"}, {"查询", "list"}, {"查看", "list"},
		{"账号", "account"}, {"账户", "account"}, {"帐户", "account"}, {"帳號", "account"},
		{"删除", "delete"}, {"修改", "update"}, {"会话", "session"}, {"命令", "command"}, {"任务", "task"},
		{"作业", "job"}, {"用户", "user"}, {"登录", "login"}, {"审计", "audit"},
	}
	for _, alias := range aliases {
		if strings.Contains(normalized, alias.source) {
			normalized += " " + alias.target
		}
	}
	raw := wordPattern.FindAllString(normalized, -1)
	seen := map[string]bool{}
	result := []string{}
	for _, group := range raw {
		for _, token := range strings.FieldsFunc(group, func(r rune) bool { return r == '_' || r == '/' || r == '-' }) {
			if len(token) > 1 && !seen[token] {
				seen[token] = true
				result = append(result, token)
			}
		}
	}
	return result
}

var sensitiveKeys = set("password", "secret", "token", "access_token", "refresh_token", "access_key", "private_key", "ssh_key", "passphrase", "cookie", "authorization", "api_key", "credential", "secret_key")
var safeMarkerKeys = set("secret_provided", "password_provided", "credential_provided")
var normalizeKeyPattern = regexp.MustCompile(`[^a-z0-9]+`)
var sensitiveTextPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?is)-----BEGIN [^-]*PRIVATE KEY-----.*?-----END [^-]*PRIVATE KEY-----`),
	regexp.MustCompile(`(?i)\bBearer\s+[A-Za-z0-9._~+/=-]+`),
	regexp.MustCompile(`\bsk-[A-Za-z0-9_-]{16,}\b`),
	regexp.MustCompile(`\bAKIA[A-Z0-9]{16}\b`),
	regexp.MustCompile(`(?i)\b(password|passphrase|secret|token|api[ _-]?key|authorization|cookie)\b\s*(?:is|[:=])\s*[^\s,;]+`),
	regexp.MustCompile(`(密码|口令|密钥|令牌|私钥)\s*(?:是|为|[:：=])\s*[^\s,，；;]+`),
}

func normalizeKey(value string) string {
	return strings.Trim(normalizeKeyPattern.ReplaceAllString(strings.ToLower(value), "_"), "_")
}
func sensitiveKey(value string) bool {
	normalized := normalizeKey(value)
	if safeMarkerKeys[normalized] {
		return false
	}
	if sensitiveKeys[normalized] {
		return true
	}
	for key := range sensitiveKeys {
		if strings.HasPrefix(normalized, key+"_") || strings.HasSuffix(normalized, "_"+key) {
			return true
		}
	}
	return false
}
func containsSensitive(value any) bool {
	switch item := value.(type) {
	case map[string]any:
		for key, child := range item {
			if sensitiveKey(key) || containsSensitive(child) {
				return true
			}
		}
	case []any:
		for _, child := range item {
			if containsSensitive(child) {
				return true
			}
		}
	}
	return false
}
func sanitize(value any, depth int) any {
	if depth > 10 {
		return "[TRUNCATED]"
	}
	switch item := value.(type) {
	case map[string]any:
		result := map[string]any{}
		for key, child := range item {
			if sensitiveKey(key) {
				result[key] = "[REDACTED]"
			} else {
				result[key] = sanitize(child, depth+1)
			}
		}
		return result
	case []any:
		limit := len(item)
		if limit > 100 {
			limit = 100
		}
		result := make([]any, limit)
		for index := 0; index < limit; index++ {
			result[index] = sanitize(item[index], depth+1)
		}
		return result
	case string:
		return boundedString(sanitizeText(item), 4096)
	default:
		return value
	}
}
func sanitizeText(value string) string {
	result := value
	for _, pattern := range sensitiveTextPatterns {
		result = pattern.ReplaceAllString(result, "[REDACTED]")
	}
	return result
}

func redactKnownSecrets(value any, request ports.CapabilityRequest) any {
	secrets := make([]string, 0, len(request.SecretInputs)+1)
	if request.AccountPassword != "" {
		secrets = append(secrets, request.AccountPassword)
	}
	for _, secret := range request.SecretInputs {
		if secret != "" {
			secrets = append(secrets, secret)
		}
	}
	var redact func(any) any
	redactText := func(value string) string {
		for _, secret := range secrets {
			value = strings.ReplaceAll(value, secret, "[REDACTED]")
		}
		return value
	}
	redact = func(item any) any {
		switch current := item.(type) {
		case string:
			return redactText(current)
		case []any:
			result := make([]any, len(current))
			for index, child := range current {
				result[index] = redact(child)
			}
			return result
		case map[string]any:
			result := make(map[string]any, len(current))
			for name, child := range current {
				result[redactText(name)] = redact(child)
			}
			return result
		default:
			return item
		}
	}
	return redact(value)
}

func stringValue(value any) string { result, _ := value.(string); return result }
func boolValue(value any) bool     { result, _ := value.(bool); return result }
func boolDefault(value any, fallback bool) bool {
	if result, ok := value.(bool); ok {
		return result
	}
	return fallback
}
func stringSlice(value any) []string {
	values := anySlice(value)
	result := make([]string, 0, len(values))
	for _, item := range values {
		if text, ok := item.(string); ok {
			result = append(result, text)
		}
	}
	return result
}
func anySlice(value any) []any { result, _ := value.([]any); return result }
func boundedString(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[:limit]
}
