// Package sqlanalysis preserves Chen's public syntax types. The parser-independent
// contracts live in dbkit; dialect parser implementations stay in Chen.
package sqlanalysis

import shared "github.com/jumpserver/koko/pkg/dbkit/sqlanalysis"

type (
	Span       = shared.Span
	Profile    = shared.Profile
	Input      = shared.Input
	Analyzer   = shared.Analyzer
	Status     = shared.Status
	Result     = shared.Result
	Kind       = shared.Kind
	Identifier = shared.Identifier
	Table      = shared.Table
	Projection = shared.Projection
	Feature    = shared.Feature
	Statement  = shared.Statement
	Code       = shared.Code
	Diagnostic = shared.Diagnostic
	Error      = shared.Error
)

const (
	Checked              = shared.Checked
	Unknown              = shared.Unknown
	Select               = shared.Select
	Insert               = shared.Insert
	Update               = shared.Update
	Delete               = shared.Delete
	Merge                = shared.Merge
	DDL                  = shared.DDL
	Transaction          = shared.Transaction
	Session              = shared.Session
	Call                 = shared.Call
	Procedural           = shared.Procedural
	Other                = shared.Other
	Join                 = shared.Join
	CTE                  = shared.CTE
	Subquery             = shared.Subquery
	SetOperation         = shared.SetOperation
	Aggregate            = shared.Aggregate
	Window               = shared.Window
	ComputedProjection   = shared.ComputedProjection
	Distinct             = shared.Distinct
	Locking              = shared.Locking
	Sequence             = shared.Sequence
	SelectInto           = shared.SelectInto
	OutputInto           = shared.OutputInto
	Assignment           = shared.Assignment
	SpecialObject        = shared.SpecialObject
	Returning            = shared.Returning
	TableModifier        = shared.TableModifier
	SyntaxError          = shared.SyntaxError
	Unsupported          = shared.Unsupported
	ProductUnknown       = shared.ProductUnknown
	MetadataInsufficient = shared.MetadataInsufficient
)
