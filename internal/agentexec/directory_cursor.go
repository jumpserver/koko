package agentexec

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
)

type directoryPosition struct {
	Version int    `json:"v"`
	Scope   string `json:"scope"`
	Offset  int    `json:"offset"`
}

func directoryScope(sessionID, path string) string {
	digest := sha256.Sum256([]byte(sessionID + "\x00" + path))
	return hex.EncodeToString(digest[:])
}

func directoryCursor(sessionID, path string, offset int) string {
	value, _ := json.Marshal(directoryPosition{Version: 1, Scope: directoryScope(sessionID, path), Offset: offset})
	return base64.RawURLEncoding.EncodeToString(value)
}

// An informational position, not an authorization capability. Paths and
// permissions are validated again on every independently audited page action.
func readDirectoryCursor(value, sessionID, path string) (int, error) {
	if value == "" {
		return 0, nil
	}
	if len(value) > 512 {
		return 0, errors.New("invalid directory cursor")
	}
	var cursor directoryPosition
	data, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil || json.Unmarshal(data, &cursor) != nil || cursor.Version != 1 || cursor.Scope != directoryScope(sessionID, path) || cursor.Offset < 1 || cursor.Offset >= 10000 {
		return 0, errors.New("invalid directory cursor; keep the same session and path")
	}
	return cursor.Offset, nil
}
