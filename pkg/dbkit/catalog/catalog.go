// Package catalog describes semantic, read-only metadata operations, not a SQL executor.
package catalog

import "context"

type Namespace struct {
	Database string `json:"database"`
	Schema   string `json:"schema"`
}

type RelationRequest struct {
	Context Namespace `json:"context"`
	Prefix  string    `json:"prefix"`
	Limit   int       `json:"limit"`
}
type ColumnRequest struct {
	Context Namespace `json:"context"`
	Schema  string    `json:"schema"`
	Table   string    `json:"table"`
}
type Relation struct {
	Database string `json:"database"`
	Schema   string `json:"schema"`
	Name     string `json:"name"`
	Kind     string `json:"kind"`
	Comment  string `json:"comment"`
}
type Column struct {
	Name     string `json:"name"`
	DataType string `json:"data_type"`
	Nullable bool   `json:"nullable"`
	Comment  string `json:"comment"`
}

// Reader is bound to an authorized session by its adapter. Implementations must
// respect context cancellation and the visibility of that account and namespace.
// Callers cannot submit arbitrary SQL or obtain credentials or a raw connection.
type Reader interface {
	Relations(context.Context, RelationRequest) ([]Relation, error)
	Columns(context.Context, ColumnRequest) ([]Column, error)
}
