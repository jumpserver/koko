package engine

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
)

// The flat JSON field names are part of the v1 cursor contract.
type catalogCursorScope struct {
	Version    int    `json:"v"`
	Session    string `json:"s"`
	Database   string `json:"d"`
	Operation  string `json:"o"`
	Collection string `json:"collection,omitempty"`
	Schema     string `json:"n,omitempty"`
	Kind       string `json:"k,omitempty"`
	Query      string `json:"q,omitempty"`
	Table      string `json:"t,omitempty"`
	Direction  string `json:"direction,omitempty"`
	Section    string `json:"section,omitempty"`
	Sort       string `json:"sort,omitempty"`
	Limit      int    `json:"limit,omitempty"`
}

type catalogPosition struct {
	AfterSchema     string `json:"as,omitempty"`
	AfterName       string `json:"an,omitempty"`
	AfterKind       string `json:"ak,omitempty"`
	AfterTable      string `json:"at,omitempty"`
	AfterOrdinal    int    `json:"ao,omitempty"`
	AfterOID        uint32 `json:"oid,omitempty"`
	ServerVersion   int    `json:"pg,omitempty"`
	Contract        string `json:"oc,omitempty"`
	DiagramContract string `json:"dc,omitempty"`
}

type catalogCursor struct {
	catalogCursorScope
	catalogPosition
}

func (req *catalogRequest) cursorScope() catalogCursorScope {
	s := req.scope
	scope := catalogCursorScope{
		Version: 1, Session: req.session.id, Database: req.database, Operation: s.operation,
		Schema: s.schema, Kind: s.kind, Query: s.query, Table: s.table,
	}
	if s.operation == "overview" || s.operation == "diagram" {
		scope.Direction, scope.Section, scope.Sort, scope.Limit = s.direction, s.section, s.sort, s.limit
	}
	if s.collection != "" {
		scope.Version, scope.Collection, scope.Limit = 2, s.collection, s.limit
	}
	return scope
}

func (c *catalogResource) signCursor(data []byte) []byte {
	mac := hmac.New(sha256.New, c.cursorKey[:])
	mac.Write(data)
	return mac.Sum(nil)
}

func (req *catalogRequest) nextCursor(after catalogPosition) string {
	data, _ := json.Marshal(catalogCursor{req.cursorScope(), after})
	return base64.RawURLEncoding.EncodeToString(data) + "." + base64.RawURLEncoding.EncodeToString(req.resource.signCursor(data))
}

func (req *catalogRequest) decodeCursor(encoded string) (catalogPosition, error) {
	var cursor catalogCursor
	payload, signature, ok := strings.Cut(encoded, ".")
	if !ok {
		return catalogPosition{}, errors.New("invalid cursor")
	}
	data, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil {
		return catalogPosition{}, err
	}
	sig, err := base64.RawURLEncoding.DecodeString(signature)
	if err != nil || !hmac.Equal(sig, req.resource.signCursor(data)) {
		return catalogPosition{}, errors.New("invalid cursor")
	}
	if json.Unmarshal(data, &cursor) != nil || cursor.catalogCursorScope != req.cursorScope() {
		return catalogPosition{}, errors.New("cursor scope mismatch")
	}
	return cursor.catalogPosition, nil
}
