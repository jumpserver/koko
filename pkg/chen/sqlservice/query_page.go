package sqlservice

import (
	"errors"
	"fmt"
	"strings"

	"github.com/jumpserver/koko/pkg/chen/dialect"
	pg "github.com/pganalyze/pg_query_go/v6"
	"github.com/pingcap/tidb/pkg/parser"
	"github.com/pingcap/tidb/pkg/parser/ast"
	"github.com/pingcap/tidb/pkg/parser/mysql"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// QueryPageSQL keeps the user's complete query as an inner query expression.
// It never removes an ORDER BY, DISTINCT, set operation or original page window.
// Admission is deliberately narrower than ordinary execution, which is unchanged.
type QueryPageRelation struct{ Schema, Name string }

type QueryPageOrder struct {
	Key, Name, Nulls string
	Ordinal          int
	Desc             bool
}
type QueryPageSQL struct {
	Scopes               []*QueryPageScope
	ProjectionKeys       []string
	Order                []QueryPageOrder
	OrderStart, OrderEnd int
	Relations            []QueryPageRelation
	CTEs                 map[string]bool
	Functions            []string
	Operators            []string
	Types                []string
	Original             string
	Ordered              bool
	Dialect              dialect.ID
	PositionalAliases    bool
	HasWindow            bool
	HasCTE               bool
	Prefix               string
	Body                 string

	// DirectTableProjection is a single table's columns or * with no expressions or clauses.
	DirectTableProjection bool
}

func AnalyzeQueryPage(d dialect.ID, document string, options AnalyzeOptions) (*QueryPageSQL, error) {
	return analyzeQueryPage(d, document, options, false)
}

// This preliminary parse selects only a possible catalog lookup. Execution
// must parse the original SQL again with the physical connection's SQL mode.
func MySQLStarHint(document string) (QueryPageRelation, bool) {
	text := strings.TrimSpace(document)
	if len(text) < len("SELECT") || !strings.EqualFold(text[:len("SELECT")], "SELECT") {
		return QueryPageRelation{}, false
	}
	nodes, _, err := parser.New().ParseSQL(text)
	if err != nil || len(nodes) != 1 {
		return QueryPageRelation{}, false
	}
	sel, ok := nodes[0].(*ast.SelectStmt)
	if !ok || sel.Fields == nil || len(sel.Fields.Fields) != 1 || sel.Fields.Fields[0].WildCard == nil {
		return QueryPageRelation{}, false
	}
	q := &QueryPageSQL{CTEs: map[string]bool{}}
	guard := &queryPageGuard{query: q}
	sel.Accept(guard)
	if guard.unsafe || guard.selects != 1 || len(q.Relations) != 1 || len(q.CTEs) != 0 {
		return QueryPageRelation{}, false
	}
	return q.Relations[0], true
}

// Explicit reexecution retains the old console's paging semantics for views,
// functions and other server-resolved sources. It still requires one SELECT
// with no nested DML, locking, INTO, parameters or executable comments. Callers
// must obtain confirmation and authorize every generated execution separately.
func AnalyzeQueryPageForReexecution(d dialect.ID, document string, options AnalyzeOptions) (*QueryPageSQL, error) {
	return analyzeQueryPage(d, document, options, true)
}

func analyzeQueryPage(d dialect.ID, document string, options AnalyzeOptions, reexecute bool) (*QueryPageSQL, error) {
	bad := errors.New("Database paging requires one repeatable SELECT; writes, locks, variables, parameters, executable hints and unverified functions are unsupported")
	if !d.UsesMySQLSyntax() && d != dialect.PostgreSQL {
		return analyzeVendorQueryPage(d, document, options, reexecute)
	}
	a, err := AnalyzeWithOptions(d, document, options)
	if err != nil {
		return nil, err
	}
	if len(a.Statements) != 1 || a.Statements[0].MayWrite || !a.Statements[0].ReturnsRows || (a.Statements[0].Kind != "select" && a.Statements[0].Kind != "setopr") {
		return nil, bad
	}
	p := &QueryPageSQL{Dialect: d, CTEs: map[string]bool{}, PositionalAliases: d == dialect.MySQL && options.MySQLVersion >= 80031}
	mode, err := mysql.GetSQLMode(options.MySQLSQLMode)
	if err != nil {
		return nil, err
	}
	// Only remove the single parser-proven statement's terminal delimiter. The
	// lexer is used for source positions, never to split or classify statements.
	source := a.Statements[0].SQL
	end := len(source)
	for _, t := range lexMode(source, d, mode.HasANSIQuotesMode(), mode.HasNoBackslashEscapesMode()) {
		if t.kind == 'c' && (strings.HasPrefix(t.text, "/*!") || !reexecute && strings.HasPrefix(t.text, "/*+") || strings.HasPrefix(strings.ToUpper(t.text), "/*M!")) {
			return nil, bad
		}
		if t.kind == ';' {
			end = t.start
			break
		}
	}
	p.Original = strings.TrimSpace(source[:end])
	if d == dialect.PostgreSQL {
		tree, err := pg.Parse(document)
		if err != nil {
			return nil, err
		}
		p.Ordered = len(tree.Stmts[0].Stmt.GetSelectStmt().SortClause) > 0
		unsafe := false
		var walk func(protoreflect.Message)
		walk = func(m protoreflect.Message) {
			switch m.Descriptor().Name() {
			case "IntoClause", "LockingClause", "InsertStmt", "UpdateStmt", "DeleteStmt", "MergeStmt", "ParamRef":
				unsafe = true
			case "RangeFunction", "RangeTableFunc", "TableSampleClause":
				unsafe = !reexecute
			case "RangeVar":
				r := m.Interface().(*pg.RangeVar)
				p.Relations = append(p.Relations, QueryPageRelation{Schema: r.Schemaname, Name: r.Relname})
			case "CommonTableExpr":
				p.CTEs[m.Interface().(*pg.CommonTableExpr).Ctename] = true
			case "TypeCast":
				names := m.Interface().(*pg.TypeCast).TypeName.Names
				if len(names) == 1 {
					p.Types = append(p.Types, names[0].GetString_().GetSval())
				}
				if !reexecute && (len(names) < 1 || len(names) > 2 || len(names) == 2 && names[0].GetString_().GetSval() != "pg_catalog" || !queryPageType(names[len(names)-1].GetString_().GetSval())) {
					unsafe = true
				}
			case "A_Expr":
				names := m.Interface().(*pg.A_Expr).Name
				if len(names) > 1 && !reexecute {
					unsafe = true
				}
				if len(names) == 1 {
					op := names[0].GetString_().GetSval()
					p.Operators = append(p.Operators, op)
					if !reexecute && !queryPageOperator(op) {
						unsafe = true
					}
				}
			case "SortBy":
				if !reexecute && len(m.Interface().(*pg.SortBy).UseOp) > 0 {
					unsafe = true
				}
			case "FuncCall":
				call := m.Interface().(*pg.FuncCall)
				names := call.Funcname
				if len(names) == 1 {
					p.Functions = append(p.Functions, names[0].GetString_().GetSval())
				}
				if !reexecute && (len(names) < 1 || len(names) > 2 || len(names) == 2 && names[0].GetString_().GetSval() != "pg_catalog" || !queryPageFunction(names[len(names)-1].GetString_().GetSval())) {
					unsafe = true
				}
			}
			if unsafe {
				return
			}
			m.Range(func(f protoreflect.FieldDescriptor, v protoreflect.Value) bool {
				if f.Kind() == protoreflect.MessageKind {
					if f.IsList() {
						for i := 0; i < v.List().Len(); i++ {
							walk(v.List().Get(i).Message())
						}
					} else {
						walk(v.Message())
					}
				}
				return !unsafe
			})
		}
		walk(tree.ProtoReflect())
		if unsafe {
			return nil, bad
		}
	} else {
		if err := ValidateBrowseSQLMode(d, options); err != nil {
			return nil, err
		}
		parser := parser.New()
		parser.SetSQLMode(mode)
		nodes, _, err := parser.ParseSQL(document)
		if err != nil {
			return nil, err
		}
		guard := &queryPageGuard{query: p, reexecute: reexecute}
		nodes[0].Accept(guard)
		if guard.unsafe {
			return nil, bad
		}
		switch n := nodes[0].(type) {
		case *ast.SelectStmt:
			p.Ordered = n.OrderBy != nil
		case *ast.SetOprStmt:
			p.Ordered = n.OrderBy != nil
		}
	}
	return p, nil
}

// An explicit built-in set rejects sequence, locking, session and arbitrary
// stored functions. The worker also checks actual relation/type provenance and
// visible function/operator overloads before enabling or replaying a source.
func queryPageFunction(name string) bool {
	switch strings.ToLower(name) {
	case "count", "sum", "min", "max", "avg", "abs", "ceil", "ceiling", "floor", "round", "length", "char_length", "lower", "upper", "coalesce", "nullif", "greatest", "least", "concat", "substring", "substr", "trim", "ltrim", "rtrim", "replace", "row_number", "rank", "dense_rank", "lag", "lead", "first_value", "last_value":
		return true
	}
	return false
}

type queryPageGuard struct {
	unsafe    bool
	selects   int
	query     *QueryPageSQL
	reexecute bool
}

func (g *queryPageGuard) Enter(n ast.Node) (ast.Node, bool) {
	switch x := n.(type) {
	case *ast.TableName:
		g.query.Relations = append(g.query.Relations, QueryPageRelation{Schema: x.Schema.O, Name: x.Name.O})
	case *ast.CommonTableExpression:
		g.query.CTEs[strings.ToLower(x.Name.O)] = true
	case *ast.SelectStmt:
		g.selects++
		if x.SelectIntoOpt != nil || x.LockInfo != nil && x.LockInfo.LockType != ast.SelectLockNone || x.SelectStmtOpts != nil && x.CalcFoundRows {
			g.unsafe = true
		}
	case *ast.VariableExpr, ast.ParamMarkerExpr:
		g.unsafe = true
	case *ast.TableOptimizerHint:
		g.unsafe = !g.reexecute
	case *ast.FuncCallExpr:
		if !g.reexecute && (x.Schema.O != "" || !queryPageFunction(x.FnName.L)) {
			g.unsafe = true
		}
	case *ast.AggregateFuncExpr:
		if !g.reexecute && !queryPageFunction(x.F) {
			g.unsafe = true
		}
	case *ast.WindowFuncExpr:
		if !g.reexecute && !queryPageFunction(x.Name) {
			g.unsafe = true
		}
	}
	return n, g.unsafe
}
func (g *queryPageGuard) Leave(n ast.Node) (ast.Node, bool) { return n, !g.unsafe }

func (p *QueryPageSQL) Page(offset, size int, columns []string) string {
	if p.PositionalAliases {
		// A derived table keeps the original LIMIT/OFFSET isolated from the UI
		// window. Positional aliases permit duplicate output names; the outer
		// projection restores their exact labels before masking/metadata binding.
		aliases, projection := make([]string, len(columns)), make([]string, len(columns))
		for i, name := range columns {
			aliases[i] = fmt.Sprintf("c%d", i)
			projection[i] = "__chen_query_page." + aliases[i] + " AS " + quoteIdentifier(name, dialect.MySQL, '`')
		}
		return fmt.Sprintf("SELECT %s FROM (\n%s\n) AS __chen_query_page(%s) LIMIT %d OFFSET %d", strings.Join(projection, ","), p.Original, strings.Join(aliases, ","), size, offset)
	}
	if p.Dialect.UsesMySQLSyntax() {
		return fmt.Sprintf("SELECT * FROM (\n%s\n) AS __chen_query_page LIMIT %d OFFSET %d", p.Original, size, offset)
	}
	if p.Dialect != dialect.PostgreSQL {
		return p.vendorPage(offset, size, columns)
	}

	return fmt.Sprintf("SELECT * FROM (\n%s\n) AS \"__chen_query_page\" LIMIT %d OFFSET %d", p.Original, size, offset)
}
func (p *QueryPageSQL) Count(columns int) string {
	if p.Dialect != dialect.PostgreSQL && !p.Dialect.UsesMySQLSyntax() {
		return p.vendorCount(columns)
	}
	aliases := make([]string, columns)
	for i := range aliases {
		aliases[i] = fmt.Sprintf("c%d", i)
	}
	suffix := ""
	if columns > 0 && (p.Dialect == dialect.PostgreSQL || p.PositionalAliases) {
		suffix = "(" + strings.Join(aliases, ",") + ")"
	}
	count := "COUNT(*)"
	if p.Dialect == dialect.PostgreSQL {
		count = "pg_catalog.count(*)"
	}
	return "SELECT " + count + " FROM (\n" + p.Original + "\n) AS __chen_query_count" + suffix
}

func queryPageType(name string) bool {
	switch strings.ToLower(name) {
	case "int2", "int4", "int8", "smallint", "integer", "bigint", "numeric", "decimal", "float4", "float8", "real", "double precision", "text", "varchar", "bpchar", "char", "bool", "boolean", "date", "time", "timetz", "timestamp", "timestamptz", "interval", "json", "jsonb", "bytea", "uuid":
		return true
	}
	return false
}

func queryPageOperator(op string) bool {
	switch op {
	case "=", "<>", "!=", "<", ">", "<=", ">=", "+", "-", "*", "/", "%", "^", "||", "~~", "!~~", "~~*", "!~~*", "~", "!~", "~*", "!~*", "->", "->>", "#>", "#>>", "@>", "<@", "?", "?|", "?&", "BETWEEN", "NOT BETWEEN":
		return true
	}
	return false
}

// Legacy MySQL-compatible derived tables cannot positionally rename duplicate
// output labels. Ordinary execution/cached export still preserves those labels.
func (p *QueryPageSQL) ValidateColumns(columns []string) error {
	for _, name := range columns {
		if p.Dialect == dialect.Oracle && strings.ContainsRune(name, '"') {
			return errors.New("Oracle cannot express a double quote inside an output SQL identifier; use an explicit compatible alias or cached results")
		}
		if name == "" && p.Dialect != dialect.PostgreSQL && !p.PositionalAliases {
			return errors.New("This database cannot wrap unnamed output columns without changing their labels; add explicit SQL aliases or keep cached results")
		}
	}
	if p.PositionalAliases || p.Dialect == dialect.PostgreSQL || p.Dialect == dialect.SQLServer || p.Dialect == dialect.DB2 || p.Dialect.UsesOracleSyntax() {
		return nil
	}
	seen := map[string]bool{}
	for _, name := range columns {
		key := strings.ToLower(name)
		if name == "" || seen[key] {
			return errors.New("This database version cannot wrap duplicate or empty result labels; use unique SQL aliases or cached results")
		}
		seen[key] = true
	}
	return nil
}
