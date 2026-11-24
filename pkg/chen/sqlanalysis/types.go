// Package sqlanalysis contains reusable syntax facts, not an execution plan or
// proof of authorization, server compatibility, or row editability.
package sqlanalysis

// Span is a half-open UTF-8 byte range in Input.SQL. Known distinguishes an
// unknown location from a real location at byte zero. Never slice unknown spans.
type Span struct {
	Start int  `json:"start"`
	End   int  `json:"end"`
	Known bool `json:"known"`
}

func (s Span) ValidFor(sql string) bool {
	return s.Known && s.Start >= 0 && s.End >= s.Start && s.End <= len(sql)
}

type Dialect string

const (
	Oracle          Dialect = "oracle"
	SQLServer       Dialect = "sqlserver"
	Dameng          Dialect = "dameng"
	OceanBaseOracle Dialect = "oceanbase-oracle"
)

// Profile records the actual product; Oracle-compatible products are not Oracle.
// Version and Mode are evidence supplied by the caller, not inferred by a parser.
type Profile struct {
	Dialect Dialect `json:"dialect"`
	Version string  `json:"version,omitempty"`
	Mode    string  `json:"mode,omitempty"`
}
type Input struct {
	SQL     string
	Profile Profile
}

type Analyzer interface{ Analyze(Input) (Result, error) }

type Status string

const (
	Checked Status = "checked"
	Unknown Status = "unknown"
)

// Result can be shared across consumers. Treat it as immutable. A successful
// parse checks grammar only; Compatibility is separate from Syntax.
type Result struct {
	Profile       Profile      `json:"profile"`
	Syntax        Status       `json:"syntax"`
	Compatibility Status       `json:"compatibility"`
	Statements    []Statement  `json:"statements"`
	Diagnostics   []Diagnostic `json:"diagnostics,omitempty"`
}

type Kind string

const (
	Select      Kind = "select"
	Insert      Kind = "insert"
	Update      Kind = "update"
	Delete      Kind = "delete"
	Merge       Kind = "merge"
	DDL         Kind = "ddl"
	Transaction Kind = "transaction"
	Session     Kind = "session"
	Call        Kind = "call"
	Procedural  Kind = "procedural"
	Other       Kind = "unknown"
)

type Identifier struct {
	Name   string `json:"name"` // unescaped; Oracle folds unquoted names, SQL Server preserves spelling
	Quoted bool   `json:"quoted"`
	Span   Span   `json:"span"`
}

type Table struct {
	Span     Span       `json:"span"`
	Server   Identifier `json:"server"`
	Database Identifier `json:"database"`
	Schema   Identifier `json:"schema"`
	Name     Identifier `json:"name"`
	Alias    Identifier `json:"alias"`
}

type Projection struct {
	Span      Span         `json:"span"`
	Qualifier []Identifier `json:"qualifier,omitempty"`
	Column    Identifier   `json:"column"`
	Label     Identifier   `json:"label"`
	Direct    bool         `json:"direct"`
	Star      bool         `json:"star"`
}

type Feature string

const (
	Join               Feature = "join"
	CTE                Feature = "cte"
	Subquery           Feature = "subquery"
	SetOperation       Feature = "set_operation"
	Aggregate          Feature = "aggregate"
	Window             Feature = "window"
	ComputedProjection Feature = "computed_projection"
	Distinct           Feature = "distinct"
	Locking            Feature = "locking"
	Sequence           Feature = "sequence"
	SelectInto         Feature = "select_into"
	OutputInto         Feature = "output_into"
	Assignment         Feature = "assignment"
	SpecialObject      Feature = "special_object"
	Returning          Feature = "returning"
	TableModifier      Feature = "table_modifier"
)

type Statement struct {
	PolicySpans []Span       `json:"policy_spans,omitempty"`
	Span        Span         `json:"span"`
	Kind        Kind         `json:"kind"`
	ReturnsRows bool         `json:"returns_rows"` // routing hint, not a server guarantee
	MayWrite    bool         `json:"may_write"`    // includes known side effects; false is not a security proof
	Tables      []Table      `json:"tables,omitempty"`
	Projections []Projection `json:"projections,omitempty"`
	Features    []Feature    `json:"features,omitempty"`
	// SourceCandidate means only that a single-table direct projection was found.
	// Base-table identity, PK, actual result labels, permissions and transaction
	// checks remain mandatory in the existing metadata/edit consumers.
	SourceCandidate bool         `json:"source_candidate"`
	Diagnostics     []Diagnostic `json:"diagnostics,omitempty"`
}
