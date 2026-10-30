package agentexec

import (
	"context"
	"errors"
	"time"
)

func (r *Runtime) query(ctx context.Context, p Principal, path string, values map[string]any) (map[string]any, error) {
	values["context_token"] = p.ContextToken
	var result map[string]any
	err := r.core.Call(ctx, path, values, &result)
	if err == nil && result == nil {
		err = errors.New("invalid recovery response")
	}
	return result, err
}

func (r *Runtime) sessionAvailability(p Principal, result map[string]any) {
	result["authorization_status"] = result["status"]
	id := stringValue(result["session_id"])
	r.mu.Lock()
	s := r.sessions[id]
	r.mu.Unlock()
	result["native_connection_available"] = false
	result["connection_status"], result["next_step"] = "context_lost", "create_session"
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.owner != owner(p) {
		return
	}
	if s.closing || result["closed"] == true || (!s.info.ExpiresAt.IsZero() && !s.info.ExpiresAt.After(time.Now())) || (s.info.Status == "active" && s.connection == nil) {
		return
	}
	for key, value := range r.publicSession(s.info) {
		if key == "connection_status" || key == "next_step" || key == "poll_after_ms" {
			result[key] = value
		}
	}
	if s.connection != nil && s.info.Status == "active" {
		result["native_connection_available"] = true
		if hints, ok := result["capabilities"].(map[string]any); ok {
			if actions, ok := hints["allowed_actions"].([]any); ok && len(actions) == 0 {
				result["next_step"] = "check_permissions"
			}
		}
	}
}

func (r *Runtime) RecoverSession(ctx context.Context, p Principal, id, key string) (map[string]any, error) {
	if (id == "") == (key == "") || (id != "" && !identifier.MatchString(id)) || (key != "" && !identifier.MatchString(key)) {
		return nil, errors.New("provide either a session_id or its original idempotency_key")
	}
	values := map[string]any{}
	if id != "" {
		values["session_id"] = id
	} else {
		values["idempotency_key"] = key
	}
	result, err := r.query(ctx, p, "sessions/get/", values)
	if err != nil {
		return nil, err
	}
	returnedID := stringValue(result["session_id"])
	if !identifier.MatchString(returnedID) || (id != "" && returnedID != id) {
		return nil, errors.New("invalid session recovery response")
	}
	result = sessionFields(result)
	r.sessionAvailability(p, result)
	return result, nil
}

func (r *Runtime) ListResources(ctx context.Context, p Principal, sessions bool, sessionID, status string, offset, limit int) (map[string]any, error) {
	if limit < 1 || limit > 100 || offset < 0 || offset > 100000 || len(status) > 32 || (sessionID != "" && !identifier.MatchString(sessionID)) {
		return nil, errors.New("invalid recovery query bounds")
	}
	path := "actions/list/"
	if sessions {
		path = "sessions/list/"
	}
	values := map[string]any{"limit": limit, "offset": offset}
	if status != "" {
		values["status"] = status
	}
	if sessionID != "" {
		values["session_id"] = sessionID
	}
	result, err := r.query(ctx, p, path, values)
	if err != nil {
		return nil, err
	}
	if sessions {
		if rows, ok := result["results"].([]any); ok {
			for i, row := range rows {
				if item, ok := row.(map[string]any); ok {
					item = sessionFields(item)
					rows[i] = item
					r.sessionAvailability(p, item)
				}
			}
		}
	}
	return result, nil
}

func sessionFields(value map[string]any) map[string]any {
	result := make(map[string]any)
	for _, key := range []string{"session_id", "action_id", "status", "asset_id", "account", "org_id", "protocol", "expires_at", "closed", "requires_terminal", "terminal_session_id", "approval_url", "audit_url", "capabilities"} {
		if item, ok := value[key]; ok {
			result[key] = item
		}
	}
	return result
}

func (r *Runtime) FindAction(ctx context.Context, p Principal, key, sessionID string) (string, error) {
	if !identifier.MatchString(key) || (sessionID != "" && !identifier.MatchString(sessionID)) {
		return "", errors.New("invalid idempotency_key or session_id")
	}
	values := map[string]any{"idempotency_key": key}
	if sessionID != "" {
		values["session_id"] = sessionID
	}
	result, err := r.query(ctx, p, "actions/lookup/", values)
	if err != nil {
		return "", err
	}
	id := stringValue(result["action_id"])
	if !identifier.MatchString(id) {
		return "", errors.New("authorization service returned an invalid action")
	}
	return id, nil
}
