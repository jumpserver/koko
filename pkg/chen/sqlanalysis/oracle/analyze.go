// Package oracle adapts Omni Oracle syntax facts without exporting its AST.
package oracle

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/bytebase/omni/oracle/ast"
	"github.com/bytebase/omni/oracle/parser"
	a "github.com/jumpserver/koko/pkg/chen/sqlanalysis"
)

type Analyzer struct{}

var _ a.Analyzer = Analyzer{}

func (Analyzer) Analyze(in a.Input) (a.Result, error) {
	out := a.Result{Profile: in.Profile, Syntax: a.Unknown, Compatibility: a.Unknown, Statements: []a.Statement{}}
	if in.Profile.Dialect != a.Oracle {
		return out, problem(a.Unsupported, "Oracle grammar is not a product adapter for this dialect", a.Span{}, nil)
	}
	if !utf8.ValidString(in.SQL) || strings.IndexByte(in.SQL, 0) >= 0 {
		return out, problem(a.SyntaxError, "SQL must be valid UTF-8 without NUL", a.Span{}, nil)
	}
	segments := parser.Split(in.SQL)
	for _, seg := range segments {
		if seg.Kind != parser.SegmentSQL {
			return out, problem(a.Unsupported, "SQL*Plus commands and embedded client source are not supported", span(seg.ByteStart, seg.ByteEnd), nil)
		}
	}
	// Tokens supply quoting and exact identifier ranges omitted by AST string
	// fields. All query structure and statement classification comes from ASTs.
	lex := parser.NewLexer(in.SQL)
	var tokens []parser.Token
	for {
		tok := lex.NextToken()
		if tok.Type == 0 {
			break
		}
		tokens = append(tokens, tok)
	}
	if lex.Err != nil {
		return out, parseError(lex.Err)
	}
	for i, tok := range tokens {
		if tok.Type == '/' && (i == 0 || strings.Contains(in.SQL[tokens[i-1].End:tok.Loc], "\n")) && (i+1 == len(tokens) || strings.Contains(in.SQL[tok.End:tokens[i+1].Loc], "\n")) {
			return out, problem(a.Unsupported, "SQL*Plus slash execution is not supported; submit ordinary SQL statements", span(tok.Loc, tok.End), nil)
		}
	}
	facts := reader{sql: in.SQL, tokens: tokens}
	for _, seg := range segments {
		if seg.Empty() {
			continue
		}
		list, err := parser.ParseRange(in.SQL, seg.ByteStart, seg.ByteEnd)
		if err != nil {
			return out, parseError(err)
		}
		if list == nil || len(list.Items) != 1 {
			return out, problem(a.Unsupported, "Expected one SQL statement per source segment", span(seg.ByteStart, seg.ByteEnd), nil)
		}
		raw, ok := list.Items[0].(*ast.RawStmt)
		if !ok || raw.Stmt == nil {
			return out, problem(a.Unsupported, "Parser did not provide a statement", span(seg.ByteStart, seg.ByteEnd), nil)
		}
		s, err := facts.statement(raw.Stmt, span(seg.ByteStart, seg.ByteEnd))
		if err != nil {
			return out, err
		}
		out.Statements = append(out.Statements, s)
	}
	out.Syntax = a.Checked
	out.Diagnostics = []a.Diagnostic{{Code: a.ProductUnknown, Message: "Oracle server version, object resolution and semantic compatibility have not been verified"}}
	return out, nil
}

func span(start, end int) a.Span {
	return a.Span{Start: start, End: end, Known: start >= 0 && end >= start}
}
func location(n ast.Node) a.Span { l := ast.NodeLoc(n); return span(l.Start, l.End) }
func problem(code a.Code, message string, loc a.Span, cause error) error {
	return &a.Error{Diagnostic: a.Diagnostic{Code: code, Message: message, Span: loc}, Cause: cause}
}
func parseError(err error) error {
	var p *parser.ParseError
	loc := a.Span{}
	if errors.As(err, &p) {
		loc = span(p.Position, p.Position)
	}
	return problem(a.SyntaxError, err.Error(), loc, err)
}

type reader struct {
	sql    string
	tokens []parser.Token
}

func (r reader) within(loc a.Span) []parser.Token {
	if !loc.ValidFor(r.sql) {
		return nil
	}
	lo := sort.Search(len(r.tokens), func(i int) bool { return r.tokens[i].Loc >= loc.Start })
	hi := sort.Search(len(r.tokens), func(i int) bool { return r.tokens[i].Loc >= loc.End })
	return r.tokens[lo:hi]
}
func (r reader) identifier(t parser.Token) a.Identifier {
	raw := r.sql[t.Loc:t.End]
	quoted := strings.HasPrefix(raw, `"`)
	name := t.Str
	if !quoted {
		name = strings.ToUpper(name)
	}
	return a.Identifier{Name: name, Quoted: quoted, Span: span(t.Loc, t.End)}
}
func (r reader) names(loc a.Span, count int) []a.Identifier {
	ts := r.within(loc)
	var out []a.Identifier
	for i := 0; i < len(ts) && len(out) < count; i++ {
		if ts[i].Type == '.' {
			continue
		}
		out = append(out, r.identifier(ts[i]))
	}
	return out
}
func (r reader) object(n *ast.ObjectName) a.Table {
	if n == nil {
		return a.Table{}
	}
	t := a.Table{Span: location(n)}
	count := 1
	if n.Schema != "" {
		count++
	}
	ns := r.names(t.Span, count)
	if len(ns) == count {
		t.Name = ns[count-1]
		if count == 2 {
			t.Schema = ns[0]
		}
	}
	return t
}
func (r reader) table(n *ast.TableRef) a.Table {
	t := r.object(n.Name)
	t.Span = location(n)
	if n.Alias != nil {
		ns := r.names(location(n.Alias), 1)
		if len(ns) == 1 {
			t.Alias = ns[0]
		}
	}
	return t
}
func add(s *a.Statement, f a.Feature) {
	for _, v := range s.Features {
		if v == f {
			return
		}
	}
	s.Features = append(s.Features, f)
}
func nonempty(l *ast.List) bool { return l != nil && len(l.Items) > 0 }

func (r reader) statement(node ast.StmtNode, loc a.Span) (a.Statement, error) {
	s := a.Statement{Span: loc, Kind: a.Other, MayWrite: true}
	functionUnknown := false
	switch n := node.(type) {
	case *ast.SelectStmt:
		s.Kind = a.Select
		s.ReturnsRows = true
		s.MayWrite = false
		r.projections(&s, n)
	case *ast.InsertStmt:
		s.Kind = a.Insert
		s.Tables = append(s.Tables, r.object(n.Table))
		if nonempty(n.Returning) {
			add(&s, a.Returning)
		}
	case *ast.UpdateStmt:
		s.Kind = a.Update
		s.Tables = append(s.Tables, r.object(n.Table))
		if nonempty(n.Returning) {
			add(&s, a.Returning)
		}
	case *ast.DeleteStmt:
		s.Kind = a.Delete
		s.Tables = append(s.Tables, r.object(n.Table))
		if nonempty(n.Returning) {
			add(&s, a.Returning)
		}
	case *ast.MergeStmt:
		s.Kind = a.Merge
		s.Tables = append(s.Tables, r.object(n.Target))
	case *ast.CommitStmt, *ast.RollbackStmt, *ast.SavepointStmt, *ast.SetTransactionStmt:
		s.Kind = a.Transaction
		s.MayWrite = false
	case *ast.SetRoleStmt, *ast.SetConstraintsStmt:
		s.Kind = a.Session
		s.MayWrite = false
	case *ast.CallStmt:
		s.Kind, s.ReturnsRows = a.Call, n.Into == nil
	case *ast.DropStmt:
		s.Kind = a.DDL
	case *ast.CreateTableStmt, *ast.AlterTableStmt, *ast.CreateIndexStmt, *ast.AlterIndexStmt, *ast.CreateViewStmt, *ast.AlterViewStmt, *ast.TruncateStmt, *ast.CommentStmt, *ast.RenameStmt:
		s.Kind = a.DDL
	default:
		// The native parser has already proved this complete statement's
		// source boundary. Preserve the console's administrative/procedural
		// SQL instead of imposing a second, ordinary-DDL-only whitelist.
		// Unknown operations stay write-capable and cannot enter read replay.
		name := strings.TrimPrefix(fmt.Sprintf("%T", node), "*ast.")
		if strings.HasPrefix(name, "Create") || strings.HasPrefix(name, "Alter") || strings.HasPrefix(name, "Drop") {
			s.Kind = a.DDL
		}
	}
	ast.Inspect(node, func(child ast.Node) bool {
		if _, ok := child.(ast.StmtNode); ok && child != node {
			s.PolicySpans = append(s.PolicySpans, location(child))
		}
		switch n := child.(type) {
		case *ast.SelectStmt:
			if n != node {
				add(&s, a.Subquery)
			}
			if n.Op != ast.SETOP_NONE {
				add(&s, a.SetOperation)
			}
			if n.WithClause != nil {
				add(&s, a.CTE)
			}
			if n.Distinct || n.UniqueKw {
				add(&s, a.Distinct)
			}
			if nonempty(n.GroupClause) || n.HavingClause != nil {
				add(&s, a.Aggregate)
			}
			if len(n.WindowDefs) > 0 {
				add(&s, a.Window)
			}
			if n.Hierarchical != nil || n.ModelClause != nil || n.QualifyClause != nil {
				add(&s, a.TableModifier)
			}
			if n.ForUpdate != nil {
				add(&s, a.Locking)
				s.MayWrite = true
			}
			if n.Into != nil || nonempty(n.IntoVars) || n.BulkCollect {
				add(&s, a.SelectInto)
				s.MayWrite = true
				if n == node {
					s.ReturnsRows = false
				}
			}
			if n.FromClause != nil && len(n.FromClause.Items) > 1 {
				add(&s, a.Join)
			}
			if n.FromClause != nil {
				for _, from := range n.FromClause.Items {
					if _, direct := from.(*ast.TableRef); !direct {
						add(&s, a.TableModifier)
					}
				}
			}
		case *ast.TableRef:
			table := r.table(n)
			s.Tables = append(s.Tables, table)
			if n.Name == nil || n.Name.DBLink != "" || n.Dblink != "" || table.Name.Name == "DUAL" {
				add(&s, a.SpecialObject)
			}
			if n.Sample != nil || n.Flashback != nil || n.PartitionExt != nil {
				add(&s, a.TableModifier)
			}
		case *ast.JoinClause:
			add(&s, a.Join)
		case *ast.ColumnRef:
			if n.OuterJoin {
				add(&s, a.Join)
			}
			ns := r.names(location(n), 3)
			if len(ns) > 0 {
				last := ns[len(ns)-1]
				if !last.Quoted && (last.Name == "NEXTVAL" || last.Name == "CURRVAL") {
					add(&s, a.Sequence)
					s.MayWrite = true
				}
			}
		case *ast.FuncCallExpr:
			if n.Over != nil {
				add(&s, a.Window)
			}
			if n.FuncName != nil {
				switch strings.ToUpper(n.FuncName.Name) {
				case "COUNT", "SUM", "AVG", "MIN", "MAX", "LISTAGG", "STDDEV", "VARIANCE", "MEDIAN", "GROUPING", "GROUPING_ID":
					add(&s, a.Aggregate)
				}
			}
			functionUnknown = true
			// A function name alone does not prove absence of side effects.
			s.MayWrite = true
		case *ast.SubqueryRef, *ast.SubqueryExpr, *ast.LateralRef:
			add(&s, a.Subquery)
		case *ast.XmlTableRef, *ast.JsonTableRef, *ast.TableCollectionExpr, *ast.ContainersExpr:
			add(&s, a.SpecialObject)
		}
		return true
	})
	s.SourceCandidate = s.Kind == a.Select && s.ReturnsRows && !s.MayWrite && len(s.Tables) == 1 && s.Tables[0].Name.Name != "" && len(s.Projections) > 0 && len(s.Features) == 0
	if s.SourceCandidate {
		t := s.Tables[0]
		for _, p := range s.Projections {
			if !p.Direct || p.Star && len(s.Projections) != 1 || !ownerMatches(p.Qualifier, t) {
				s.SourceCandidate = false
				break
			}
		}
	}
	if functionUnknown {
		s.Diagnostics = append(s.Diagnostics, a.Diagnostic{Code: a.MetadataInsufficient, Message: "Function side effects have not been verified; may_write remains conservative", Span: s.Span})
	}
	if s.SourceCandidate {
		s.Diagnostics = append(s.Diagnostics, a.Diagnostic{Code: a.MetadataInsufficient, Message: "Resolve the base table, primary key and actual result columns before editing", Span: s.Span})
	} else {
		reason := "Editing requires one local table and unambiguous direct columns"
		if len(s.Features) > 0 {
			reason += fmt.Sprintf("; features: %v", s.Features)
		}
		s.Diagnostics = append(s.Diagnostics, a.Diagnostic{Code: a.Unsupported, Message: reason, Span: s.Span})
	}
	return s, nil
}
func ownerMatches(q []a.Identifier, t a.Table) bool {
	if len(q) == 0 {
		return true
	}
	owner := t.Name.Name
	if t.Alias.Name != "" {
		owner = t.Alias.Name
	}
	if len(q) == 1 {
		return q[0].Name == owner
	}
	return len(q) == 2 && t.Alias.Name == "" && q[0].Name == t.Schema.Name && q[1].Name == t.Name.Name
}
func (r reader) projections(s *a.Statement, n *ast.SelectStmt) {
	if n.TargetList == nil {
		return
	}
	for _, item := range n.TargetList.Items {
		target, ok := item.(*ast.ResTarget)
		if !ok {
			add(s, a.ComputedProjection)
			continue
		}
		p := a.Projection{Span: location(target)}
		switch col := target.Expr.(type) {
		case *ast.ColumnRef:
			count := 1
			if col.Table != "" {
				count++
			}
			if col.Schema != "" {
				count++
			}
			ns := r.names(location(col), count)
			if len(ns) == count {
				p.Direct = true
				p.Column = ns[count-1]
				p.Qualifier = ns[:count-1]
				p.Star = col.Column == "*" && !p.Column.Quoted
			}
		case *ast.Star:
			p.Direct = true
			p.Star = true
		}
		p.Label = p.Column
		if target.Name != "" {
			ts := r.within(location(target))
			if len(ts) > 0 {
				p.Label = r.identifier(ts[len(ts)-1])
			}
		}
		if !p.Direct {
			add(s, a.ComputedProjection)
		}
		s.Projections = append(s.Projections, p)
	}
}
