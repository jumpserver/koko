package sqlservice

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"unicode/utf8"
)

type RoutineParameter struct {
	CharacterSet string        `json:"character_set,omitempty"`
	Collation    string        `json:"collation,omitempty"`
	Position     int           `json:"position"`
	Name         string        `json:"name,omitempty"`
	Mode         string        `json:"mode"`
	Type         ObjectTypeRef `json:"type"`
	DataType     string        `json:"data_type"`
	TypeKind     string        `json:"type_kind,omitempty"`
	HasDefault   bool          `json:"has_default"`
	Default      *string       `json:"default,omitempty"`
}

type ObjectDetail struct {
	ObjectSummary
	Parameters []RoutineParameter `json:"parameters,omitempty"`
	Properties map[string]any     `json:"properties"`
	Related    []ObjectSummary    `json:"related,omitempty"`
	Actions    []string           `json:"actions"`
}

type ObjectDefinition struct {
	NextOffset       int               `json:"next_offset,omitempty"`
	DefaultPositions []int             `json:"default_positions,omitempty"`
	Environment      map[string]string `json:"environment,omitempty"`
	Ref              ObjectRef         `json:"ref"`
	OID              uint32            `json:"oid,omitempty"`
	Status           string            `json:"status"`
	Text             string            `json:"text,omitempty"`
	Source           string            `json:"source"`
	Version          string            `json:"version,omitempty"`
}

// The offset counts Unicode characters in the native server definition.
type DefinitionPageRequest struct {
	Offset  int    `json:"offset,omitempty"`
	Version string `json:"version,omitempty"`
}

func (p DefinitionPageRequest) Valid() bool {
	if p.Version == "" {
		return p.Offset == 0
	}
	if len(p.Version) != sha256.Size*2 || p.Offset < 0 || p.Offset >= 1<<30 {
		return false
	}
	_, err := hex.DecodeString(p.Version)
	return err == nil
}

const definitionPageCharacters = 256 << 10

// Complete definitions stay available to the editor. Each page of a larger
// definition remains explicitly read-only, including its last page.
func (d *ObjectDefinition) setPage(page DefinitionPageRequest, body string, total int) error {
	if page.Version != "" && page.Version != d.Version || page.Offset >= total {
		return ErrObjectChanged
	}
	count := 0
	for i := range body {
		if count == definitionPageCharacters {
			body = body[:i]
			break
		}
		count++
	}
	d.Status, d.Text = "truncated", body
	if next := page.Offset + count; next < total {
		d.NextOffset = next
	}
	return nil
}

func definitionFingerprint(body string) string {
	sum := sha256.Sum256([]byte(body))
	return hex.EncodeToString(sum[:])
}

// ValidObjectRef bounds a logical identity before allocating database resources.
func ValidObjectRef(ref ObjectRef) bool {
	valid := func(s string) bool {
		return s != "" && utf8.ValidString(s) && utf8.RuneCountInString(s) <= 256 && !strings.ContainsRune(s, 0)
	}
	if !valid(ref.Database) || !valid(ref.Schema) || !valid(ref.Name) || len(ref.InputTypes) > 100 {
		return false
	}
	for _, t := range ref.InputTypes {
		if !valid(t.Name) || !valid(t.Schema) {
			return false
		}
	}
	if ref.Kind == TriggerKind {
		return valid(ref.Table) && len(ref.InputTypes) == 0
	}
	if ref.Kind == EventKind {
		return ref.Table == "" && len(ref.InputTypes) == 0
	}
	return ref.Kind.IsRoutine() && ref.Table == ""
}
