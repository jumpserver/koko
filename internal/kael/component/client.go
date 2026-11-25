package component

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/jumpserver-dev/sdk-go/httplib"
	kaelmodel "github.com/jumpserver/koko/internal/kael/model"
	"go.uber.org/zap"
)

const (
	componentType      = "kael"
	registerPath       = "/api/v1/terminal/terminal-registrations/"
	profilePath        = "/api/v1/users/profile/"
	terminalConfigPath = "/api/v1/terminal/terminals/config/"
	heartbeatPath      = "/api/v1/terminal/terminals/status/"
	runtimeStorePath   = "/api/v1/chat-ai/runtime-store/"
	openAPIPath        = "/api/swagger.json"
	maxOpenAPIBytes    = int64(32 * 1024 * 1024)
	connectAttempts    = 10
	connectRetryDelay  = 5 * time.Second
)

var errInvalidAccessKey = errors.New("component access key file is invalid")

var (
	ErrRuntimeStoreRevisionConflict = errors.New("runtime store revision conflict")
	ErrRuntimeStoreCommitUncertain  = errors.New("runtime store commit outcome is uncertain")
	ErrRuntimeStoreUnavailable      = errors.New("runtime store request was not sent")
)

type Options struct {
	CoreURL        string
	TLSVerify      bool
	Timeout        time.Duration
	Name           string
	BootstrapToken string
	AccessKeyFile  string
	Logger         *zap.Logger
}

type Client struct {
	mu              sync.Mutex
	client          *httplib.Client
	openAPIClient   *http.Client
	coreURL         string
	accessKeyID     string
	accessKeySecret string
}

type accessKey struct {
	ID     string
	Secret string
}

type registrationResponse struct {
	ServiceAccount struct {
		AccessKey accessKey `json:"access_key"`
	} `json:"service_account"`
}

type terminalConfig struct {
	ChatAIEnabled  *bool  `json:"CHAT_AI_ENABLED"`
	ChatAIProvider string `json:"CHAT_AI_PROVIDER"`
	ChatAIBaseURL  string `json:"CHAT_AI_BASE_URL"`
	ChatAIAPIKey   string `json:"CHAT_AI_API_KEY"`
	ChatAIProxy    string `json:"CHAT_AI_PROXY"`
	ChatAIModel    string `json:"CHAT_AI_MODEL"`
}

type RuntimeStoreRecord struct {
	Revision uint64 `json:"revision"`
	Snapshot bool   `json:"snapshot"`
	Record   string `json:"record"`
	CommitID string `json:"commit_id"`
}

type RuntimeStorePage struct {
	Revision uint64               `json:"revision"`
	Nonce    string               `json:"nonce"`
	Results  []RuntimeStoreRecord `json:"results"`
	HasMore  bool                 `json:"has_more"`
	Receipt  string               `json:"receipt"`
}

type runtimeStoreAppendRequest struct {
	ExpectedRevision uint64 `json:"expected_revision"`
	Snapshot         bool   `json:"snapshot"`
	Record           string `json:"record"`
	CommitID         string `json:"commit_id"`
	Integrity        string `json:"integrity"`
}

type runtimeStoreAppendResponse struct {
	AuditVersion    int    `json:"audit_version"`
	Revision        uint64 `json:"revision"`
	CurrentRevision uint64 `json:"current_revision"`
	Code            string `json:"code"`
	CommitID        string `json:"commit_id"`
	Receipt         string `json:"receipt"`
}

func Connect(options Options) (*Client, error) {
	return connect(options, time.Sleep)
}

func connect(options Options, wait func(time.Duration)) (*Client, error) {
	if strings.TrimSpace(options.Name) == "" || strings.TrimSpace(options.AccessKeyFile) == "" {
		return nil, fmt.Errorf("component identity is incomplete")
	}
	if options.Logger == nil {
		options.Logger = zap.NewNop()
	}
	if options.Timeout < 30*time.Second {
		options.Timeout = 30 * time.Second
	}
	if err := os.MkdirAll(filepath.Dir(options.AccessKeyFile), 0o700); err != nil {
		return nil, fmt.Errorf("create component key directory: %w", err)
	}
	key, err := loadAccessKey(options.AccessKeyFile)
	if err != nil && !errors.Is(err, os.ErrNotExist) && !errors.Is(err, errInvalidAccessKey) {
		return nil, err
	}
	if err == nil {
		client, clientErr := authenticatedClient(options, key)
		if clientErr != nil {
			return nil, clientErr
		}
		var valid bool
		validationErr := retryConnect(options.Logger, wait, func() error {
			var err error
			valid, err = validateAccessKey(client)
			return err
		})
		if validationErr != nil {
			return nil, validationErr
		}
		if valid {
			return connectedClient(options, client, key), nil
		}
		options.Logger.Warn("Kael component access key unauthorized; registering a new access key")
	}
	if strings.TrimSpace(options.BootstrapToken) == "" {
		return nil, fmt.Errorf("component access key is missing or unauthorized; set BOOTSTRAP_TOKEN to register Kael")
	}
	err = retryConnect(options.Logger, wait, func() error {
		var registerErr error
		key, registerErr = register(options)
		return registerErr
	})
	if err != nil {
		return nil, err
	}
	if err = saveAccessKey(options.AccessKeyFile, key); err != nil {
		return nil, err
	}
	client, err := authenticatedClient(options, key)
	if err != nil {
		return nil, err
	}
	return connectedClient(options, client, key), nil
}

func retryConnect(logger *zap.Logger, wait func(time.Duration), request func() error) error {
	var err error
	for attempt := 1; attempt <= connectAttempts; attempt++ {
		if err = request(); err == nil {
			return nil
		}
		logger.Warn("Core component request failed", zap.Int("attempt", attempt), zap.Int("max_attempts", connectAttempts), zap.Error(err))
		if attempt < connectAttempts {
			wait(connectRetryDelay)
		}
	}
	return fmt.Errorf("connect to Core failed after %d attempts: %w", connectAttempts, err)
}

func connectedClient(options Options, client *httplib.Client, key accessKey) *Client {
	schemaTimeout := options.Timeout
	if schemaTimeout < 90*time.Second {
		schemaTimeout = 90 * time.Second
	}
	return &Client{
		client:          client,
		openAPIClient:   &http.Client{Timeout: schemaTimeout, Transport: httplib.NewTransport(!options.TLSVerify)},
		coreURL:         strings.TrimRight(options.CoreURL, "/"),
		accessKeyID:     key.ID,
		accessKeySecret: key.Secret,
	}
}

func register(options Options) (accessKey, error) {
	client, err := newHTTPClient(options)
	if err != nil {
		return accessKey{}, err
	}
	client.SetAuthSign(&httplib.CustomAuth{AuthScheme: "BootstrapToken", Token: options.BootstrapToken})
	request := map[string]string{"name": options.Name, "comment": componentType, "type": componentType}
	var response registrationResponse
	httpResponse, err := client.Post(registerPath, request, &response)
	if err != nil {
		return accessKey{}, responseError("register Kael component", httpResponse, err)
	}
	key := response.ServiceAccount.AccessKey
	if key.ID == "" || key.Secret == "" {
		return accessKey{}, fmt.Errorf("register Kael component: Core returned an empty access key")
	}
	return key, nil
}

func authenticatedClient(options Options, key accessKey) (*httplib.Client, error) {
	client, err := newHTTPClient(options)
	if err != nil {
		return nil, err
	}
	client.SetHeader("X-JMS-ORG", "ROOT")
	client.SetAuthSign(&httplib.SigAuth{KeyID: key.ID, SecretID: key.Secret})
	return client, nil
}

func newHTTPClient(options Options) (*httplib.Client, error) {
	settings := make([]httplib.Opt, 0, 1)
	if !options.TLSVerify {
		settings = append(settings, httplib.WithInsecure())
	}
	client, err := httplib.NewClient(options.CoreURL, options.Timeout, settings...)
	if err != nil {
		return nil, fmt.Errorf("create Core component client: %w", err)
	}
	return client, nil
}

func validateAccessKey(client *httplib.Client) (bool, error) {
	var profile struct {
		ID string `json:"id"`
	}
	response, err := client.Get(profilePath, &profile)
	if response != nil && response.StatusCode == http.StatusUnauthorized {
		return false, nil
	}
	if err != nil {
		return false, responseError("validate Kael component access key", response, err)
	}
	if profile.ID == "" {
		return false, fmt.Errorf("validate Kael component access key: Core returned an empty profile")
	}
	return true, nil
}

func (c *Client) ModelConfig(ctx context.Context) (kaelmodel.Config, error) {
	if err := ctx.Err(); err != nil {
		return kaelmodel.Config{}, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	var value terminalConfig
	response, err := c.client.Get(terminalConfigPath, &value)
	if err != nil {
		return kaelmodel.Config{}, responseError("load model configuration from TerminalConfig", response, err)
	}
	return modelConfig(value)
}

// OpenAPISchema loads Core's API registry with the component identity. Core's
// schema endpoint is intentionally authenticated, so platform capabilities must
// not fetch it through an anonymous HTTP client.
func (c *Client) OpenAPISchema(ctx context.Context) (map[string]any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.coreURL+openAPIPath, nil)
	if err != nil {
		return nil, fmt.Errorf("load Core OpenAPI schema: create request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-JMS-ORG", "ROOT")
	request.Header.Set("X-JMS-AI-Schema", "1")
	if err = (&httplib.SigAuth{KeyID: c.accessKeyID, SecretID: c.accessKeySecret}).Sign(request); err != nil {
		return nil, fmt.Errorf("load Core OpenAPI schema: sign request: %w", err)
	}
	response, err := c.openAPIClient.Do(request)
	if err != nil {
		return nil, responseError("load Core OpenAPI schema", response, err)
	}
	if response == nil {
		return nil, responseError("load Core OpenAPI schema", nil, fmt.Errorf("empty response"))
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, responseError("load Core OpenAPI schema", response, fmt.Errorf("unexpected response"))
	}
	content, err := io.ReadAll(io.LimitReader(response.Body, maxOpenAPIBytes+1))
	if err != nil {
		return nil, fmt.Errorf("load Core OpenAPI schema: read response: %w", err)
	}
	if int64(len(content)) > maxOpenAPIBytes {
		return nil, fmt.Errorf("load Core OpenAPI schema: response exceeds 32 MiB limit")
	}
	var value map[string]any
	if err = json.Unmarshal(content, &value); err != nil {
		return nil, fmt.Errorf("load Core OpenAPI schema: invalid JSON response: %w", err)
	}
	if value == nil {
		return nil, fmt.Errorf("load Core OpenAPI schema: invalid JSON response")
	}
	return value, nil
}

func (c *Client) LoadRuntimeStore(after uint64, limit int) (RuntimeStorePage, error) {
	return c.LoadRuntimeStoreContext(context.Background(), after, limit)
}

func (c *Client) LoadRuntimeStoreContext(ctx context.Context, after uint64, limit int) (RuntimeStorePage, error) {
	if err := ctx.Err(); err != nil {
		return RuntimeStorePage{}, err
	}
	if limit < 1 || limit > 1000 {
		return RuntimeStorePage{}, fmt.Errorf("runtime store page limit must be between 1 and 1000")
	}
	nonce := uuid.NewString()
	query := url.Values{
		"after": []string{fmt.Sprintf("%d", after)},
		"limit": []string{fmt.Sprintf("%d", limit)},
		"nonce": []string{nonce},
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.coreURL+runtimeStorePath+"?"+query.Encode(), nil)
	if err != nil {
		return RuntimeStorePage{}, fmt.Errorf("load Kael runtime store: create request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-JMS-ORG", "ROOT")
	if err = (&httplib.SigAuth{KeyID: c.accessKeyID, SecretID: c.accessKeySecret}).Sign(request); err != nil {
		return RuntimeStorePage{}, fmt.Errorf("load Kael runtime store: sign request: %w", err)
	}
	response, err := c.openAPIClient.Do(request)
	if err != nil {
		return RuntimeStorePage{}, responseError("load Kael runtime store", response, err)
	}
	if response == nil {
		return RuntimeStorePage{}, responseError("load Kael runtime store", nil, fmt.Errorf("empty response"))
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return RuntimeStorePage{}, responseError("load Kael runtime store", response, fmt.Errorf("unexpected response"))
	}
	var value RuntimeStorePage
	decoder := json.NewDecoder(response.Body)
	if err = decoder.Decode(&value); err != nil {
		return RuntimeStorePage{}, fmt.Errorf("load Kael runtime store: decode response: %w", err)
	}
	var extra any
	if err = decoder.Decode(&extra); err != io.EOF {
		return RuntimeStorePage{}, fmt.Errorf("load Kael runtime store: response must contain one JSON value")
	}
	if value.Nonce != nonce {
		return RuntimeStorePage{}, fmt.Errorf("load Kael runtime store: Core returned another nonce")
	}
	canonical, err := runtimeStorePageCanonical(nonce, after, limit, value)
	if err != nil {
		return RuntimeStorePage{}, fmt.Errorf("load Kael runtime store: %w", err)
	}
	if !validRuntimeStoreHMAC(c.accessKeySecret, canonical, value.Receipt) {
		return RuntimeStorePage{}, fmt.Errorf("load Kael runtime store: Core page receipt is invalid")
	}
	return value, nil
}

func (c *Client) AppendRuntimeStore(commitID string, expectedRevision uint64, snapshot bool, record string) (uint64, error) {
	if _, err := uuid.Parse(commitID); err != nil {
		return 0, fmt.Errorf("append Kael runtime store: commit ID is invalid")
	}
	integrity := runtimeStoreIntegrity(c.accessKeySecret, commitID, expectedRevision, snapshot, record)
	payload := runtimeStoreAppendRequest{ExpectedRevision: expectedRevision, Snapshot: snapshot, Record: record, CommitID: commitID, Integrity: integrity}
	body, err := json.Marshal(payload)
	if err != nil {
		return 0, fmt.Errorf("append Kael runtime store: encode request: %w", err)
	}
	// The standard transport only retries this non-replayable POST when the
	// previous attempt could not commit (for example, an idle connection wrote
	// zero bytes). Track the final attempt; redirects must stay disabled below.
	var gotConn atomic.Bool
	ctx := httptrace.WithClientTrace(context.Background(), &httptrace.ClientTrace{
		GetConn: func(string) { gotConn.Store(false) },
		GotConn: func(httptrace.GotConnInfo) { gotConn.Store(true) },
	})
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.coreURL+runtimeStorePath, bytes.NewReader(body))
	if err != nil {
		return 0, fmt.Errorf("append Kael runtime store: create request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-JMS-ORG", "ROOT")
	if err = (&httplib.SigAuth{KeyID: c.accessKeyID, SecretID: c.accessKeySecret}).Sign(request); err != nil {
		return 0, fmt.Errorf("append Kael runtime store: sign request: %w", err)
	}
	client := *c.openAPIClient
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	response, err := client.Do(request)
	if err != nil {
		if !gotConn.Load() {
			return 0, fmt.Errorf("%w: %w", ErrRuntimeStoreUnavailable, err)
		}
		return 0, fmt.Errorf("%w: %w", ErrRuntimeStoreCommitUncertain, err)
	}
	defer response.Body.Close()
	responseBody, readErr := io.ReadAll(io.LimitReader(response.Body, 64*1024+1))
	if readErr != nil || len(responseBody) > 64*1024 {
		return 0, fmt.Errorf("%w: unable to read bounded Core commit response", ErrRuntimeStoreCommitUncertain)
	}
	var value runtimeStoreAppendResponse
	err = json.Unmarshal(responseBody, &value)
	if response.StatusCode >= http.StatusOK && response.StatusCode < http.StatusMultipleChoices {
		if err != nil {
			return 0, fmt.Errorf("%w: decode Core response: %v", ErrRuntimeStoreCommitUncertain, err)
		}
		if response.StatusCode != http.StatusCreated {
			return 0, fmt.Errorf("%w: Core returned HTTP %d", ErrRuntimeStoreCommitUncertain, response.StatusCode)
		}
		if value.Revision != expectedRevision+1 {
			return 0, fmt.Errorf("%w: Core returned revision %d, expected %d", ErrRuntimeStoreCommitUncertain, value.Revision, expectedRevision+1)
		}
		if value.CommitID != commitID {
			return 0, fmt.Errorf("%w: Core returned another commit ID", ErrRuntimeStoreCommitUncertain)
		}
		digest := sha256.Sum256([]byte(record))
		canonical := fmt.Sprintf("kael-runtime-store-receipt-v1\ndefault\n%s\n%d\n%d\n%d\n%s", commitID, expectedRevision, value.Revision, runtimeStoreBool(snapshot), hex.EncodeToString(digest[:]))
		if !validRuntimeStoreHMAC(c.accessKeySecret, canonical, value.Receipt) {
			return 0, fmt.Errorf("%w: Core receipt is invalid", ErrRuntimeStoreCommitUncertain)
		}
		if value.AuditVersion != 1 {
			return 0, fmt.Errorf("%w: Core does not support conversation audit ingestion; upgrade Core first", ErrRuntimeStoreCommitUncertain)
		}
		return value.Revision, nil
	}
	if response.StatusCode == http.StatusBadRequest {
		return 0, fmt.Errorf("append Kael runtime store: Core returned HTTP 400: %s", runtimeStoreValidationError(responseBody))
	}
	if response.StatusCode == http.StatusConflict {
		return 0, fmt.Errorf("%w: expected %d, current %d, code %q", ErrRuntimeStoreRevisionConflict, expectedRevision, value.CurrentRevision, value.Code)
	}
	if response.StatusCode >= http.StatusInternalServerError {
		return 0, fmt.Errorf("%w: Core returned HTTP %d", ErrRuntimeStoreCommitUncertain, response.StatusCode)
	}
	if err != nil {
		return 0, responseError("append Kael runtime store", response, err)
	}
	return 0, responseError("append Kael runtime store", response, fmt.Errorf("Core returned HTTP %d", response.StatusCode))
}

// Only include the known validation-error fields, never response headers or
// the submitted journal. Non-JSON (for example Django debug HTML) is omitted.
func runtimeStoreValidationError(body []byte) string {
	var fields map[string]json.RawMessage
	if json.Unmarshal(body, &fields) != nil {
		return "invalid request (non-JSON response)"
	}
	for key := range fields {
		switch key {
		case "detail", "record", "commit_id", "expected_revision", "snapshot", "integrity", "non_field_errors":
		default:
			delete(fields, key)
		}
	}
	detail, err := json.Marshal(fields)
	if err != nil || len(fields) == 0 {
		return "invalid request"
	}
	if len(detail) > 2048 {
		return string(detail[:2048]) + "..."
	}
	return string(detail)
}

func runtimeStoreIntegrity(secret, commitID string, expectedRevision uint64, snapshot bool, record string) string {
	digest := sha256.Sum256([]byte(record))
	canonical := fmt.Sprintf("kael-runtime-store-commit-v1\ndefault\n%s\n%d\n%d\n%s", commitID, expectedRevision, runtimeStoreBool(snapshot), hex.EncodeToString(digest[:]))
	return runtimeStoreHMAC(secret, canonical)
}

func runtimeStorePageCanonical(nonce string, after uint64, limit int, page RuntimeStorePage) (string, error) {
	var canonical strings.Builder
	fmt.Fprintf(&canonical, "kael-runtime-store-page-v1\ndefault\n%s\n%d\n%d\n%d\n%d\n%d", nonce, after, limit, page.Revision, runtimeStoreBool(page.HasMore), len(page.Results))
	for _, record := range page.Results {
		if record.Revision == 0 {
			return "", fmt.Errorf("record revision is invalid")
		}
		if _, err := uuid.Parse(record.CommitID); err != nil {
			return "", fmt.Errorf("record revision %d commit ID is invalid", record.Revision)
		}
		digest := sha256.Sum256([]byte(record.Record))
		fmt.Fprintf(&canonical, "\n%d\n%s\n%d\n%s", record.Revision, record.CommitID, runtimeStoreBool(record.Snapshot), hex.EncodeToString(digest[:]))
	}
	return canonical.String(), nil
}

func runtimeStoreBool(value bool) int {
	if value {
		return 1
	}
	return 0
}

func runtimeStoreHMAC(secret, canonical string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(canonical))
	return hex.EncodeToString(mac.Sum(nil))
}

func validRuntimeStoreHMAC(secret, canonical, provided string) bool {
	return equalHexDigest(runtimeStoreHMAC(secret, canonical), provided)
}

func equalHexDigest(expected, provided string) bool {
	expectedBytes, expectedErr := hex.DecodeString(expected)
	providedBytes, providedErr := hex.DecodeString(provided)
	return expectedErr == nil && providedErr == nil && hmac.Equal(expectedBytes, providedBytes)
}

func modelConfig(value terminalConfig) (kaelmodel.Config, error) {
	if value.ChatAIEnabled == nil || !*value.ChatAIEnabled {
		return kaelmodel.Config{}, fmt.Errorf("Chat AI is disabled in TerminalConfig")
	}
	config := kaelmodel.Config{
		Provider:        strings.ToLower(strings.TrimSpace(value.ChatAIProvider)),
		BaseURL:         strings.TrimSpace(value.ChatAIBaseURL),
		APIKey:          strings.TrimSpace(value.ChatAIAPIKey),
		Model:           strings.TrimSpace(value.ChatAIModel),
		Proxy:           strings.TrimSpace(value.ChatAIProxy),
		ReasoningEffort: "low",
		Timeout:         5 * time.Minute,
	}
	if config.Provider == "" {
		config.Provider = "openai_compatible"
	}
	if config.BaseURL == "" || config.APIKey == "" || config.Model == "" {
		return kaelmodel.Config{}, fmt.Errorf("model endpoint is incomplete in TerminalConfig")
	}
	return config, nil
}

func (c *Client) Heartbeat(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	payload := map[string]any{"sessions": []string{}, "session_online": 0, "cpu_load": 0, "memory_used": 0, "disk_used": 0}
	var tasks []map[string]any
	response, err := c.client.Post(heartbeatPath, payload, &tasks)
	if err != nil {
		return responseError("send Kael component heartbeat", response, err)
	}
	return nil
}

func (c *Client) RunHeartbeat(ctx context.Context, logger *zap.Logger) {
	if logger == nil {
		logger = zap.NewNop()
	}
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		if err := c.Heartbeat(ctx); err != nil && ctx.Err() == nil {
			logger.Warn("Kael component heartbeat failed", zap.Error(err))
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func loadAccessKey(path string) (accessKey, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return accessKey{}, err
	}
	parts := strings.SplitN(strings.TrimSpace(string(data)), ":", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return accessKey{}, errInvalidAccessKey
	}
	return accessKey{ID: parts[0], Secret: parts[1]}, nil
}

func saveAccessKey(path string, key accessKey) error {
	temporary, err := os.CreateTemp(filepath.Dir(path), ".access_key-")
	if err != nil {
		return fmt.Errorf("create component access key: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err = temporary.Chmod(0o600); err == nil {
		_, err = temporary.WriteString(key.ID + ":" + key.Secret)
	}
	if err == nil {
		err = temporary.Sync()
	}
	if closeErr := temporary.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return fmt.Errorf("write component access key: %w", err)
	}
	if err = os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("replace component access key: %w", err)
	}
	return nil
}

func responseError(action string, response *http.Response, err error) error {
	if response == nil {
		return fmt.Errorf("%s: Core is unavailable: %w", action, err)
	}
	return fmt.Errorf("%s: Core returned HTTP %d", action, response.StatusCode)
}
