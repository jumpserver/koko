// Package agentexec owns transport-independent, audited asset operations.
package agentexec

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/jumpserver-dev/sdk-go/httplib"
	"github.com/jumpserver-dev/sdk-go/model"
)

const corePrefix = "/api/v1/terminal/agent/"

var identifier = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,128}$`)

// Only translate public Core codes; never reflect credential error bodies.
var publicErrorHints = map[string]string{
	"sftp_session_required":  "Use a separate connect_asset session with protocol=sftp and virtual paths rooted at /.",
	"shell_session_required": "execute_command requires a shell or database session.",
	"shell_context_required": "cwd and env require a shell session.",
	"connection_not_ready":   "Query the connection action_id and complete any returned approval before submitting operations.",
	"file_content_too_large": "Use file_write(operation=upload) for content larger than 64 KiB.",
	"request_too_large":      "The encoded request exceeds its size limit; use file transfer for large content.",
	"invalid_action_request": "Correct the action fields and encoding before submitting again.",
	"invalid_request":        "Correct the request parameters before submitting again.",
	"idempotency_conflict":   "Keep the original key for the identical operation; use a new key only for a new operation.",
	"capacity_exceeded":      "Wait for existing work to finish or close an unused session.",
	"invalid_lookup":         "Provide either an existing resource ID or its original idempotency key.",
	"ambiguous_lookup":       "The key exists in more than one organization; provide its resource ID.",
}

// Principal is issued by Core after validating a resource-bound OAuth token.
// ContextToken must never be returned to an MCP client or included in logs.
type Principal struct {
	UserID           string    `json:"user_id"`
	OrgID            string    `json:"org_id"`
	AllOrganizations bool      `json:"all_organizations,omitempty"`
	ClientID         string    `json:"client_id"`
	ContextToken     string    `json:"context_token"`
	ExpiresAt        time.Time `json:"expires_at"`
	Resource         string    `json:"resource"`
	Scopes           []string  `json:"scopes"`
	RemoteAddr       string    `json:"-"`
}

type Session struct {
	ID                  string         `json:"id"`
	Status              string         `json:"status"`
	AuthorizationStatus string         `json:"authorization_status,omitempty"`
	ActionID            string         `json:"action_id"`
	SessionToken        string         `json:"session_token,omitempty"`
	TerminalSessionID   string         `json:"terminal_session_id,omitempty"`
	Protocol            string         `json:"protocol,omitempty"`
	ApprovalURL         string         `json:"approval_url,omitempty"`
	ExpiresAt           time.Time      `json:"expires_at,omitempty"`
	RequiresTerminal    bool           `json:"requires_terminal,omitempty"`
	Capabilities        map[string]any `json:"capabilities,omitempty"`
}

type Action struct {
	ID                   string         `json:"id"`
	ActionID             string         `json:"action_id"`
	SessionID            string         `json:"session_id"`
	Kind                 string         `json:"kind"`
	Status               string         `json:"status"`
	Request              map[string]any `json:"request,omitempty"`
	RequestHash          string         `json:"request_hash,omitempty"`
	Result               map[string]any `json:"result,omitempty"`
	ApprovalURL          string         `json:"approval_url,omitempty"`
	CancelRequested      bool           `json:"cancel_requested,omitempty"`
	AuditStatus          string         `json:"audit_status,omitempty"`
	Events               []Event        `json:"events,omitempty"`
	NextOffset           *int64         `json:"next_offset"`
	EvidencePending      bool           `json:"evidence_pending"`
	LastSequence         uint64         `json:"last_sequence,omitempty"`
	HandoffURL           string         `json:"handoff_url,omitempty"`
	AuditURL             string         `json:"audit_url,omitempty"`
	NextStep             string         `json:"next_step,omitempty"`
	Terminal             bool           `json:"is_terminal"`
	OutputComplete       bool           `json:"output_complete"`
	PollAfterMS          int            `json:"poll_after_ms,omitempty"`
	NextCursor           string         `json:"next_cursor,omitempty"`
	Output               []OutputStream `json:"output,omitempty"`
	OutputLossy          bool           `json:"output_lossy,omitempty"`
	OutputRequiresCursor bool           `json:"output_requires_cursor,omitempty"`
	Transfer             *Transfer      `json:"transfer,omitempty"`
}

func terminalState(s string) bool {
	switch s {
	case "succeeded", "failed", "cancelled", "rejected", "expired", "unknown":
		return true
	}
	return false
}

// Core is also the test seam for authorization and the durable dispatch claim.
type Core interface {
	Call(context.Context, string, any, any) error
}

type CoreClient struct {
	base   string
	client *http.Client
	sign   httplib.SigAuth
}

func NewCoreClient(base string, key model.AccessKey) (*CoreClient, error) {
	u, err := url.Parse(base)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || key.ID == "" || key.Secret == "" {
		return nil, errors.New("valid Core address and registered component credentials are required")
	}
	return &CoreClient{base: strings.TrimRight(base, "/"), sign: httplib.SigAuth{KeyID: key.ID, SecretID: key.Secret},
		client: &http.Client{Timeout: 10 * time.Second, Transport: httplib.NewTransport(false), CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}

func (c *CoreClient) Call(ctx context.Context, path string, body, result any) error {
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+corePrefix+path, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-JMS-ORG", "ROOT")
	if err = c.sign.Sign(req); err != nil {
		return err
	}
	response, err := c.client.Do(req)
	if err != nil {
		return &CoreError{Status: http.StatusServiceUnavailable, Operation: path}
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		// Only recognize fixed, public validation codes. Never reflect an
		// arbitrary error body: credential endpoints can contain secrets.
		failure := &CoreError{Status: response.StatusCode, Operation: path}
		if seconds, parseErr := strconv.Atoi(response.Header.Get("Retry-After")); parseErr == nil && seconds > 0 {
			failure.RetryAfterMS = min(seconds, 60) * 1000
		}
		data, _ := io.ReadAll(io.LimitReader(response.Body, 8192))
		failure.Code = publicCoreError(data)
		clear(data)
		return failure
	}
	data, err = io.ReadAll(io.LimitReader(response.Body, (5<<20)+1))
	if err != nil || len(data) > 5<<20 {
		return &CoreError{Status: http.StatusBadGateway, Operation: path}
	}
	defer clear(data)
	if result != nil && json.Unmarshal(data, result) != nil {
		return &CoreError{Status: http.StatusBadGateway, Operation: path}
	}
	return nil
}

type CoreError struct {
	Status       int
	Operation    string
	Code         string
	RetryAfterMS int
}

func (e *CoreError) Error() string {
	_, hint := e.guidance()
	return fmt.Sprintf("authorization service %s returned HTTP %d: %s", e.Operation, e.Status, hint)
}

func publicCoreError(data []byte) string {
	var detail struct {
		Code string `json:"code"`
	}
	if json.Unmarshal(data, &detail) == nil {
		if _, public := publicErrorHints[detail.Code]; public {
			return detail.Code
		}
	}
	return ""
}

func (e *CoreError) guidance() (string, string) {
	if hint, ok := publicErrorHints[e.Code]; ok {
		return e.Code, hint
	}
	if e.Operation == "sessions/" && e.Status == http.StatusForbidden {
		return "asset_not_authorized", "Use the exact search_assets results[].id and accounts[].id (including virtual aliases), not account names. Check the account's authorized protocols and organization; do not repeat unchanged parameters."
	}
	if e.Operation == "sessions/" && e.Status == http.StatusNotFound {
		return "asset_not_found", "Refresh search_assets and use the current asset/account IDs before connecting; cached references may have changed."
	}
	return "core_request_rejected", "Do not automatically replay this request. Check parameters, authorization and any known action_id; keep the original idempotency key for the identical operation."
}

func operationPath(kind, id, operation string) (string, error) {
	if !identifier.MatchString(id) {
		return "", errors.New("invalid resource identifier")
	}
	return kind + "/" + id + "/" + operation + "/", nil
}

func decodeRequest(value map[string]any, out any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, out)
}
