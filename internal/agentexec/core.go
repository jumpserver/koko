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
	"strings"
	"time"

	"github.com/jumpserver-dev/sdk-go/httplib"
	"github.com/jumpserver-dev/sdk-go/model"
)

const corePrefix = "/api/v1/terminal/agent/"

var identifier = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,128}$`)

// Principal is issued by Core after validating a resource-bound OAuth token.
// ContextToken must never be returned to an MCP client or included in logs.
type Principal struct {
	UserID       string    `json:"user_id"`
	OrgID        string    `json:"org_id"`
	ClientID     string    `json:"client_id"`
	ContextToken string    `json:"context_token"`
	ExpiresAt    time.Time `json:"expires_at"`
	Resource     string    `json:"resource"`
	Scopes       []string  `json:"scopes"`
	RemoteAddr   string    `json:"-"`
}

type Session struct {
	ID                string    `json:"id"`
	Status            string    `json:"status"`
	ActionID          string    `json:"action_id"`
	SessionToken      string    `json:"session_token,omitempty"`
	TerminalSessionID string    `json:"terminal_session_id,omitempty"`
	Protocol          string    `json:"protocol,omitempty"`
	ApprovalURL       string    `json:"approval_url,omitempty"`
	ExpiresAt         time.Time `json:"expires_at,omitempty"`
	RequiresTerminal  bool      `json:"requires_terminal,omitempty"`
}

type Action struct {
	ID              string         `json:"id"`
	ActionID        string         `json:"action_id"`
	SessionID       string         `json:"session_id"`
	Kind            string         `json:"kind"`
	Status          string         `json:"status"`
	Request         map[string]any `json:"request,omitempty"`
	RequestHash     string         `json:"request_hash,omitempty"`
	Result          map[string]any `json:"result,omitempty"`
	ApprovalURL     string         `json:"approval_url,omitempty"`
	CancelRequested bool           `json:"cancel_requested,omitempty"`
	AuditStatus     string         `json:"audit_status,omitempty"`
	Events          []Event        `json:"events,omitempty"`
	NextOffset      *int64         `json:"next_offset"`
	EvidencePending bool           `json:"evidence_pending"`
	LastSequence    uint64         `json:"last_sequence,omitempty"`
	HandoffURL      string         `json:"handoff_url,omitempty"`
	AuditURL        string         `json:"audit_url,omitempty"`
	NextStep        string         `json:"next_step,omitempty"`
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
		// Only recognize fixed, public validation messages. Never reflect an
		// arbitrary error body: credential endpoints can contain secrets.
		failure := &CoreError{Status: response.StatusCode, Operation: path}
		data, _ := io.ReadAll(io.LimitReader(response.Body, 8192))
		var detail any
		if json.Unmarshal(data, &detail) == nil {
			failure.Code = publicCoreError(detail, 0)
		}
		clear(data)
		return failure
	}
	data, err = io.ReadAll(io.LimitReader(response.Body, (5<<20)+1))
	if err != nil || len(data) > 5<<20 {
		return errors.New("invalid or oversized authorization service response")
	}
	defer clear(data)
	if result != nil && json.Unmarshal(data, result) != nil {
		return errors.New("invalid authorization service response")
	}
	return nil
}

type CoreError struct {
	Status    int
	Operation string
	Code      string
}

func (e *CoreError) Error() string {
	_, hint := e.guidance()
	return fmt.Sprintf("authorization service %s returned HTTP %d: %s", e.Operation, e.Status, hint)
}

func publicCoreError(value any, depth int) string {
	if depth > 4 {
		return ""
	}
	switch value := value.(type) {
	case string:
		switch value {
		case "File operations require an SFTP session":
			return "sftp_session_required"
		case "Asset connection is not ready":
			return "connection_not_ready"
		case "This operation requires a shell or database session":
			return "shell_session_required"
		}
	case []any:
		for _, item := range value {
			if code := publicCoreError(item, depth+1); code != "" {
				return code
			}
		}
	case map[string]any:
		for _, key := range []string{"detail", "error", "msg"} {
			if code := publicCoreError(value[key], depth+1); code != "" {
				return code
			}
		}
	}
	return ""
}

func (e *CoreError) guidance() (string, string) {
	switch e.Code {
	case "sftp_session_required":
		return e.Code, "File operations require a separate connect_asset session with protocol=sftp. Use virtual SFTP paths rooted at /; SSH filesystem paths may differ."
	case "connection_not_ready":
		return e.Code, "Query the connection action_id until succeeded; complete any returned human approval before submitting operations."
	case "shell_session_required":
		return e.Code, "execute_command requires an SSH or database session; an SFTP session only supports file operations."
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
