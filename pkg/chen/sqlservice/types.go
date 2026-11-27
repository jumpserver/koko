// Package sqlservice supplies dialect-aware parsing and authorized catalog-backed
// completion. An AST classifies syntax; it is never an authorization or read-only
// guarantee. No user SQL is executed by completion.
package sqlservice

import (
	"errors"

	"github.com/jumpserver/koko/pkg/chen/dialect"
	"github.com/jumpserver/koko/pkg/chen/sqlanalysis"
	"github.com/jumpserver/koko/pkg/dbkit/catalog"
)

const (
	MaxDocumentCodeUnits = 4 << 20 // Java: 1,024 chunks of 4,096 UTF-16 code units.
	MaxDocumentBytes     = 3 * MaxDocumentCodeUnits
	MaxCompletionItems   = 200
)

// DocumentExceedsLimit preserves the Java chunk transport's text capacity for
// both ASCII and multibyte SQL. UTF-8 bytes alone would reject valid CJK scripts.
func DocumentExceedsLimit(document string) bool {
	if len(document) <= MaxDocumentCodeUnits {
		return false
	}
	if len(document) > MaxDocumentBytes {
		return true
	}
	units := 0
	for _, r := range document {
		units++
		if r > 0xffff {
			units++
		}
		if units > MaxDocumentCodeUnits {
			return true
		}
	}
	return false
}

func upperCaseIdentifiers(d dialect.ID) bool { return d.UsesOracleSyntax() || d == dialect.DB2 }

var (
	ErrUnsupportedDialect = errors.New("unsupported SQL dialect")
	ErrDocumentTooLarge   = errors.New("SQL document exceeds 4,194,304 UTF-16 code units")
)

type Context = catalog.Namespace

type Statement struct {
	PolicySQL []string `json:"-"`
	// Facts is immutable, reusable syntax provenance, never an edit permission.
	Facts       *sqlanalysis.Statement `json:"facts,omitempty"`
	SQL         string                 `json:"sql"`
	Start       int                    `json:"start"` // UTF-8 byte offset in the original document.
	End         int                    `json:"end"`
	Kind        string                 `json:"kind"`
	ReturnsRows bool                   `json:"returns_rows"`
	MayWrite    bool                   `json:"may_write"` // Advisory classification, never a read-only or authorization guarantee.
}
type Analysis struct {
	Syntax        sqlanalysis.Status       `json:"syntax"`
	Compatibility sqlanalysis.Status       `json:"compatibility"`
	Diagnostics   []sqlanalysis.Diagnostic `json:"diagnostics,omitempty"`
	Statements    []Statement              `json:"statements"`
}

type AnalyzeOptions struct {
	MySQLSQLMode string
	MySQLVersion int // Server version as major*10000+minor*100+patch. Zero means 80000.
}

type MetadataRequest = catalog.RelationRequest
type ColumnRequest = catalog.ColumnRequest
type Relation = catalog.Relation
type Column = catalog.Column

type CompletionRequest struct {
	Document    string  `json:"document"`
	UTF16Offset int     `json:"position_utf16"`
	Version     int     `json:"document_version"`
	Context     Context `json:"context"`
	// Options must be supplied from the server connection, never the client.
	Options AnalyzeOptions `json:"-"`
}
type CompletionResponse struct {
	Version int              `json:"document_version"`
	From    int              `json:"from"`
	To      int              `json:"to"`
	Items   []CompletionItem `json:"items"`
}
type CompletionItem struct {
	Label         string `json:"label"`
	InsertText    string `json:"insert_text"`
	Kind          string `json:"kind"`
	Detail        string `json:"detail"`
	Documentation string `json:"documentation"`
	FilterText    string `json:"filter_text,omitempty"`
	SortText      string `json:"sort_text,omitempty"`
}

type Catalog = catalog.Reader
