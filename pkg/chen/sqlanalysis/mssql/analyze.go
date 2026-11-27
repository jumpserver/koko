// Package mssql adapts T-SQL AST facts; it does not implement client batches.
package mssql

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/bytebase/omni/mssql/ast"
	"github.com/bytebase/omni/mssql/parser"
	"github.com/jumpserver/koko/pkg/dbkit/dialect"
	a "github.com/jumpserver/koko/pkg/dbkit/sqlanalysis"
)

type Analyzer struct{}

var _ a.Analyzer = Analyzer{}

func (Analyzer) Analyze(in a.Input) (a.Result, error) {
	out := a.Result{Profile: in.Profile, Syntax: a.Unknown, Compatibility: a.Unknown, Statements: []a.Statement{}}
	if in.Profile.Dialect != dialect.SQLServer {
		return out, problem(a.Unsupported, "T-SQL adapter requires SQL Server", a.Span{}, nil)
	}
	if !utf8.ValidString(in.SQL) || strings.IndexByte(in.SQL, 0) >= 0 {
		return out, problem(a.SyntaxError, "SQL must be valid UTF-8 without NUL", a.Span{}, nil)
	}
	lex := parser.NewLexer(in.SQL)
	var tokens []parser.Token
	for {
		t := lex.NextToken()
		if t.Type == 0 {
			break
		}
		tokens = append(tokens, t)
	}
	for i, t := range tokens {
		if t.Type == ':' && (i == 0 || strings.Contains(in.SQL[tokens[i-1].End:t.Loc], "\n")) {
			return out, problem(a.Unsupported, "SQLCMD client commands are not supported", span(t.Loc, t.End), nil)
		}
	}
	// MSSQL has no ranged Split/Parse entry. Parse the document once, using its
	// absolute AST locations; require explicit semicolons between top-level nodes.
	// This retains Chen's existing execution boundary for implicit T-SQL batches.
	tree, err := parser.Parse(in.SQL)
	if err != nil {
		return out, parseError(err)
	}
	if lex.Err != nil {
		return out, parseError(lex.Err)
	}
	// A T-SQL routine definition owns the remainder of its server batch,
	// including semicolons. Never dispatch a parsed trailing body statement
	// as a separate top-level command.
	for _, node := range tree.Items {
		switch node.(type) {
		case *ast.CreateProcedureStmt, *ast.CreateFunctionStmt, *ast.CreateTriggerStmt, *ast.CreateViewStmt:
			if len(tree.Items) != 1 {
				return out, problem(a.Unsupported, "Submit a routine or view definition in its own SQL execution", location(node), nil)
			}
		}
	}
	r := reader{sql: in.SQL, tokens: tokens}
	previousEnd, cursor := 0, 0
	for i, node := range tree.Items {
		loc := location(node)
		if !loc.ValidFor(in.SQL) || loc.Start < previousEnd {
			return out, problem(a.Unsupported, "Parser did not provide a valid statement range", loc, nil)
		}
		if _, goCommand := node.(*ast.GoStmt); goCommand {
			return out, problem(a.Unsupported, "GO batches and repetition are not supported; submit semicolon-delimited SQL", loc, nil)
		}
		if i > 0 {
			separator := false
			for _, t := range r.within(span(previousEnd, loc.Start)) {
				if t.Type == ';' {
					separator = true
				}
			}
			if !separator {
				return out, problem(a.Unsupported, "Separate top-level SQL statements with semicolons; implicit batches are not supported", loc, nil)
			}
		}
		end := loc.End
		next := sort.Search(len(tokens), func(i int) bool { return tokens[i].Loc >= end })
		if next < len(tokens) && tokens[next].Type == ';' {
			end = tokens[next].End
		}
		s, err := r.statement(node, span(cursor, end))
		if err != nil {
			return out, err
		}
		// Double quotes can mean strings when QUOTED_IDENTIFIER is OFF.
		// A grammar-only parse cannot prove the live session setting, including
		// SET statements earlier in this document. Keep these sources read-only.
		for _, token := range r.within(s.Span) {
			if strings.HasPrefix(in.SQL[token.Loc:token.End], `"`) {
				s.SourceCandidate = false
				s.Diagnostics = append(s.Diagnostics, a.Diagnostic{Code: a.Unsupported, Message: "Double-quoted sources require a verified QUOTED_IDENTIFIER session setting before editing", Span: span(token.Loc, token.End)})
				break
			}
		}
		out.Statements = append(out.Statements, s)
		previousEnd, cursor = loc.End, end
	}
	out.Syntax = a.Checked
	out.Diagnostics = []a.Diagnostic{{Code: a.ProductUnknown, Message: "SQL Server version, compatibility level, session quoting and collation have not been verified"}}
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
	return a.Identifier{Name: t.Str, Quoted: strings.HasPrefix(raw, "[") || strings.HasPrefix(raw, `"`), Span: span(t.Loc, t.End)}
}

// qualified reads identifier spelling from a name node's already-proven range.
// Empty parts are retained (database..table); this is not a SQL parser.
func (r reader) qualified(loc a.Span) ([]a.Identifier, int) {
	ts := r.within(loc)
	if len(ts) == 0 {
		return nil, 0
	}
	ids := []a.Identifier{r.identifier(ts[0])}
	i := 1
	for i < len(ts) && ts[i].Type == '.' {
		i++
		if i == len(ts) || ts[i].Type == '.' {
			ids = append(ids, a.Identifier{})
			continue
		}
		ids = append(ids, r.identifier(ts[i]))
		i++
	}
	return ids, i
}
func (r reader) table(n *ast.TableRef) a.Table {
	t := a.Table{Span: location(n)}
	ids, used := r.qualified(t.Span)
	if len(ids) > 0 && len(ids) <= 4 {
		t.Name = ids[len(ids)-1]
		if len(ids) > 1 {
			t.Schema = ids[len(ids)-2]
		}
		if len(ids) > 2 {
			t.Database = ids[len(ids)-3]
		}
		if len(ids) > 3 {
			t.Server = ids[0]
		}
	}
	if n.Alias != "" {
		ts := r.within(t.Span)
		for _, tok := range ts[used:] {
			if tok.Str == n.Alias {
				t.Alias = r.identifier(tok)
				break
			}
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
func output(s *a.Statement, o *ast.OutputClause) {
	if o == nil {
		return
	}
	add(s, a.Returning)
	s.ReturnsRows = o.IntoTable == nil
	if o.IntoTable != nil {
		add(s, a.OutputInto)
	}
}

func (r reader) statement(node ast.Node, loc a.Span) (a.Statement, error) {
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
		output(&s, n.OutputClause)
	case *ast.UpdateStmt:
		s.Kind = a.Update
		output(&s, n.OutputClause)
	case *ast.DeleteStmt:
		s.Kind = a.Delete
		output(&s, n.OutputClause)
	case *ast.MergeStmt:
		s.Kind = a.Merge
		output(&s, n.OutputClause)
	case *ast.ExecStmt:
		s.Kind = a.Call
		s.ReturnsRows = true
	case *ast.BeginTransStmt, *ast.CommitTransStmt, *ast.RollbackTransStmt, *ast.SaveTransStmt:
		s.Kind = a.Transaction
		s.MayWrite = false
	case *ast.UseStmt, *ast.SetStmt, *ast.SetOptionStmt:
		s.Kind = a.Session
		s.MayWrite = false
	case *ast.DropStmt:
		s.Kind = a.DDL
	case *ast.CreateTableStmt, *ast.AlterTableStmt, *ast.CreateIndexStmt, *ast.AlterIndexStmt, *ast.CreateViewStmt, *ast.TruncateStmt:
		s.Kind = a.DDL
	default:
		// Parsed administrative statements retain their original source and
		// policy checks. Conservative MayWrite excludes them from paging,
		// export and estimated-plan admission, independently of execution.
		name := strings.TrimPrefix(fmt.Sprintf("%T", node), "*ast.")
		if strings.HasPrefix(name, "Create") || strings.HasPrefix(name, "Alter") || strings.HasPrefix(name, "Drop") {
			s.Kind = a.DDL
		}
		switch node.(type) {
		case *ast.BeginEndStmt, *ast.IfStmt, *ast.WhileStmt, *ast.TryCatchStmt, *ast.FetchCursorStmt, *ast.DbccStmt, *ast.ReadtextStmt, *ast.ReceiveStmt:
			s.ReturnsRows = true
		}
	}
	// Function and sequence names use TableRef nodes too; they are not tables.
	nameNodes := map[*ast.TableRef]bool{}
	ast.Inspect(node, func(child ast.Node) bool {
		if _, ok := child.(ast.StmtNode); ok && child != node {
			s.PolicySpans = append(s.PolicySpans, location(child))
		}
		switch n := child.(type) {
		case *ast.SelectStmt:
			if n != node {
				add(&s, a.Subquery)
			}
			if n.Op != ast.SetOpNone {
				add(&s, a.SetOperation)
			}
			if n.Distinct {
				add(&s, a.Distinct)
			}
			if nonempty(n.GroupByClause) || n.HavingClause != nil {
				add(&s, a.Aggregate)
			}
			if nonempty(n.WindowClause) {
				add(&s, a.Window)
			}
			if n.IntoTable != nil {
				add(&s, a.SelectInto)
				s.MayWrite = true
				if n == node {
					s.ReturnsRows = false
				}
			}
			if n.ForClause != nil || nonempty(n.OptionClause) {
				add(&s, a.TableModifier)
			}
			if n.FromClause != nil {
				if len(n.FromClause.Items) > 1 {
					add(&s, a.Join)
				}
				for _, from := range n.FromClause.Items {
					if _, ok := from.(*ast.TableRef); !ok {
						add(&s, a.TableModifier)
					}
				}
			}
		case *ast.WithClause:
			add(&s, a.CTE)
		case *ast.TableRef:
			if nameNodes[n] {
				return false
			}
			tb := r.table(n)
			s.Tables = append(s.Tables, tb)
			if n.Server != "" || strings.HasPrefix(n.Object, "#") || strings.HasPrefix(n.Object, "@") {
				add(&s, a.SpecialObject)
			}
			if n.Temporal != nil || nonempty(n.Hints) {
				add(&s, a.TableModifier)
			}
		case *ast.TableHint:
			switch strings.ToUpper(n.Name) {
			case "UPDLOCK", "XLOCK", "HOLDLOCK", "TABLOCKX", "SERIALIZABLE":
				add(&s, a.Locking)
				s.MayWrite = true
			}
		case *ast.JoinClause:
			add(&s, a.Join)
		case *ast.SubqueryExpr:
			add(&s, a.Subquery)
		case *ast.TableVarRef, *ast.TableVarMethodCallRef, *ast.AdHocTableRef, *ast.FullTextTableRef, *ast.SemanticTableRef:
			add(&s, a.SpecialObject)
		case *ast.SelectAssign:
			add(&s, a.Assignment)
			s.MayWrite = true
			if s.Kind == a.Select {
				s.ReturnsRows = false
			}
		case *ast.SetExpr:
			if n.Variable != "" {
				add(&s, a.Assignment)
			}
		case *ast.NextValueForExpr:
			nameNodes[n.Sequence] = true
			add(&s, a.Sequence)
			s.MayWrite = true
		case *ast.FuncCallExpr:
			nameNodes[n.Name] = true
			if n.Over != nil {
				add(&s, a.Window)
			}
			if n.Name != nil {
				switch strings.ToUpper(n.Name.Object) {
				case "COUNT", "COUNT_BIG", "SUM", "AVG", "MIN", "MAX", "STRING_AGG", "STDEV", "VAR", "GROUPING", "GROUPING_ID":
					add(&s, a.Aggregate)
				}
			}
			functionUnknown = true
			s.MayWrite = true
		}
		return true
	})
	s.SourceCandidate = s.Kind == a.Select && s.ReturnsRows && !s.MayWrite && len(s.Tables) == 1 && s.Tables[0].Name.Name != "" && len(s.Projections) > 0 && len(s.Features) == 0
	if s.SourceCandidate {
		tb := s.Tables[0]
		for _, p := range s.Projections {
			if !p.Direct || p.Star && len(s.Projections) != 1 || !ownerMatches(p.Qualifier, tb) {
				s.SourceCandidate = false
				break
			}
		}
	}
	if functionUnknown {
		s.Diagnostics = append(s.Diagnostics, a.Diagnostic{Code: a.MetadataInsufficient, Message: "Function side effects have not been verified; may_write remains conservative", Span: s.Span})
	}
	if s.SourceCandidate {
		s.Diagnostics = append(s.Diagnostics, a.Diagnostic{Code: a.MetadataInsufficient, Message: "Resolve the base table, collation, primary key and actual result columns before editing", Span: s.Span})
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
	// Exact spelling is sufficient evidence; case-insensitive resolution requires
	// the database collation. Never fold SQL Server identities in the Adapter.
	if len(q) == 1 {
		return q[0].Name == owner
	}
	if t.Alias.Name != "" {
		return false
	}
	return len(q) == 2 && q[0].Name == t.Schema.Name && q[1].Name == t.Name.Name ||
		len(q) == 3 && q[0].Name == t.Database.Name && q[1].Name == t.Schema.Name && q[2].Name == t.Name.Name
}
func (r reader) projections(s *a.Statement, n *ast.SelectStmt) {
	if n.TargetList == nil {
		return
	}
	for _, item := range n.TargetList.Items {
		rt, ok := item.(*ast.ResTarget)
		if !ok {
			add(s, a.ComputedProjection)
			continue
		}
		p := a.Projection{Span: location(rt)}
		switch col := rt.Val.(type) {
		case *ast.ColumnRef:
			ids, _ := r.qualified(location(col))
			if len(ids) > 0 {
				p.Direct = true
				p.Column = ids[len(ids)-1]
				p.Qualifier = ids[:len(ids)-1]
			}
		case *ast.StarExpr:
			p.Direct = true
			p.Star = true
			if col.Qualifier != "" {
				ids, _ := r.qualified(location(col))
				if len(ids) > 1 {
					p.Qualifier = ids[:len(ids)-1]
				}
			}
		}
		p.Label = p.Column
		if rt.Name != "" {
			expr := location(rt.Val)
			for _, t := range r.within(location(rt)) {
				if (t.Loc < expr.Start || t.Loc >= expr.End) && t.Str == rt.Name {
					p.Label = r.identifier(t)
					break
				}
			}
		}
		if !p.Direct {
			add(s, a.ComputedProjection)
		}
		s.Projections = append(s.Projections, p)
	}
}
