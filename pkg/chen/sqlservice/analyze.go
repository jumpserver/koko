package sqlservice

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/jumpserver/koko/pkg/chen/sqlanalysis"
	mssqlanalysis "github.com/jumpserver/koko/pkg/chen/sqlanalysis/mssql"
	oracleanalysis "github.com/jumpserver/koko/pkg/chen/sqlanalysis/oracle"

	pg "github.com/pganalyze/pg_query_go/v6"
	"github.com/pingcap/tidb/pkg/parser"
	"github.com/pingcap/tidb/pkg/parser/ast"
	"github.com/pingcap/tidb/pkg/parser/mysql"
	_ "github.com/pingcap/tidb/pkg/parser/test_driver"
	"google.golang.org/protobuf/reflect/protoreflect"
)

func Analyze(dialect Dialect, document string) (Analysis, error) {
	return AnalyzeWithOptions(dialect, document, AnalyzeOptions{})
}

// AnalyzeWithOptions uses native syntax parsers for Oracle/SQL Server/PostgreSQL/MySQL. Other
// dialects use a bounded statement scanner; their server validates SQL
// grammar at execution. Neither path is an authorization boundary.
// Statements always refer to original UTF-8 byte spans; no AST serialization is
// used for execution. MySQL DELIMITER is a client directive and is rejected.
// PostgreSQL SQL/plpgsql function bodies require server semantic validation in
// addition to this syntactic analysis (extensions may install other languages).
func AnalyzeWithOptions(dialect Dialect, document string, options AnalyzeOptions) (Analysis, error) {
	result := Analysis{Statements: []Statement{}, Syntax: sqlanalysis.Unknown, Compatibility: sqlanalysis.Unknown}
	if !supported(dialect) {
		return result, ErrUnsupportedDialect
	}
	if DocumentExceedsLimit(document) {
		return result, ErrDocumentTooLarge
	}
	if dialect == Oracle {
		facts, err := (oracleanalysis.Analyzer{}).Analyze(sqlanalysis.Input{SQL: document, Profile: sqlanalysis.Profile{Dialect: sqlanalysis.Oracle}})
		return analysisFromFacts(document, facts, err)
	}
	if dialect == SQLServer {
		facts, err := (mssqlanalysis.Analyzer{}).Analyze(sqlanalysis.Input{SQL: document, Profile: sqlanalysis.Profile{Dialect: sqlanalysis.SQLServer}})
		return analysisFromFacts(document, facts, err)
	}
	if !utf8.ValidString(document) {
		return result, fmt.Errorf("SQL document is not valid UTF-8")
	}
	if strings.IndexByte(document, 0) >= 0 {
		return result, fmt.Errorf("SQL document contains NUL")
	}
	if (dialect == Dameng || dialect == OceanBaseOracle) && oracleUnitDocument(dialect, document) {
		// Oracle-compatible procedural units need the grammar's complete
		// source boundaries, not the ordinary scanner's semicolon splitting.
		// The actual product still validates its own SQL at execution.
		facts, err := (oracleanalysis.Analyzer{}).Analyze(sqlanalysis.Input{SQL: document, Profile: sqlanalysis.Profile{Dialect: sqlanalysis.Oracle}})
		return analysisFromFacts(document, facts, err)
	}
	if dialect == ClickHouse && strings.HasPrefix(document, "EXPLAIN PLAN ") {
		body := strings.TrimPrefix(document, "EXPLAIN PLAN ")
		if strings.HasPrefix(document, ClickHouseJSONPlanPrefix) {
			body = strings.TrimPrefix(document, ClickHouseJSONPlanPrefix)
		}
		inner, err := planReadDocument(dialect, body, options)
		if err != nil || len(inner.Statements) != 1 {
			return result, fmt.Errorf("ClickHouse estimated plans require one read query")
		}
		inner.Statements = []Statement{{SQL: document, Start: 0, End: len(document), Kind: "explain", ReturnsRows: true}}
		return inner, nil
	}
	if dialect != PostgreSQL && !IsMySQLDialect(dialect) {
		return analyzeCommandDocument(dialect, document, options)
	}
	if dialect == PostgreSQL {
		tree, err := pg.Parse(document)
		if err != nil {
			return result, fmt.Errorf("PostgreSQL syntax: %w", err)
		}
		for _, node := range tree.Stmts {
			start, end := int(node.StmtLocation), len(document)
			if node.StmtLen > 0 {
				end = start + int(node.StmtLen)
			}
			if start < 0 || end < start || end > len(document) {
				return result, fmt.Errorf("invalid parser source span")
			}
			kind := "unknown"
			if node.Stmt != nil && node.Stmt.Node != nil {
				kind = strings.TrimSuffix(strings.TrimPrefix(reflect.TypeOf(node.Stmt.Node).String(), "*pg_query.Node_"), "Stmt")
			}
			if f := node.Stmt.GetCreateFunctionStmt(); f != nil {
				for _, option := range f.Options {
					opt := option.GetDefElem()
					if opt != nil && opt.Defname == "language" && opt.Arg.GetString_().GetSval() == "plpgsql" {
						if _, err := pg.ParsePlPgSqlToJSON(document[start:end]); err != nil {
							return result, fmt.Errorf("PL/pgSQL syntax: %w", err)
						}
					}
				}
			}
			returnsRows, mayWrite := pgStatementCapabilities(node.Stmt)
			result.Statements = append(result.Statements, Statement{SQL: document[start:end], Start: start, End: end, Kind: strings.ToLower(kind), ReturnsRows: returnsRows, MayWrite: mayWrite})
		}
		result.Syntax = sqlanalysis.Checked
		return result, nil
	}
	input, err := mysqlParserInput(document, options)
	if err != nil {
		return result, err
	}
	p := parser.New()
	mode := options.MySQLSQLMode
	if mode == "" {
		mode = mysql.DefaultSQLMode
	}
	sqlMode, err := mysql.GetSQLMode(mode)
	if err != nil {
		return result, fmt.Errorf("invalid MySQL SQL mode: %w", err)
	}
	p.SetSQLMode(sqlMode)
	nodes, _, err := p.ParseSQL(input)
	if err != nil {
		if routines, routineErr := analyzeMySQLRoutines(document, input, sqlMode, p); routineErr == nil {
			return routines, nil
		}
		// MariaDB and OceanBase extend the MySQL grammar. The bounded scanner
		// preserves per-statement policy checks for their ordinary DML/DDL.
		if dialect == MariaDB || dialect == OceanBaseMySQL {
			return analyzeCommandDocument(dialect, document, options)
		}
		return result, fmt.Errorf("MySQL syntax: %w", err)
	}
	cursor := 0
	for _, node := range nodes {
		text := node.OriginalText()
		if text == "" {
			return result, fmt.Errorf("MySQL parser omitted original statement text")
		}
		relative := strings.Index(input[cursor:], text)
		if relative < 0 {
			return result, fmt.Errorf("MySQL parser source span mismatch")
		}
		start := cursor + relative
		end := start + len(text)
		cursor = end
		kind := strings.ToLower(strings.TrimSuffix(reflect.TypeOf(node).Elem().Name(), "Stmt"))
		returnsRows, mayWrite := mysqlStatementCapabilities(node)
		result.Statements = append(result.Statements, Statement{SQL: document[start:end], Start: start, End: end, Kind: kind, ReturnsRows: returnsRows, MayWrite: mayWrite})
	}
	result.Syntax = sqlanalysis.Checked
	return result, nil
}

func oracleUnitDocument(d Dialect, document string) bool {
	var ts []token
	for _, t := range lex(document, d) {
		if t.kind != 'c' {
			ts = append(ts, t)
		}
		if len(ts) >= 5 {
			break
		}
	}
	if len(ts) == 0 {
		return false
	}
	if commandIs(ts[0], "BEGIN", "DECLARE") {
		return true
	}
	if !ts[0].is("CREATE") {
		return false
	}
	pos := 1
	if len(ts) > 3 && ts[1].is("OR") && ts[2].is("REPLACE") {
		pos = 3
	}
	return pos < len(ts) && commandIs(ts[pos], "PROCEDURE", "FUNCTION", "PACKAGE", "TRIGGER", "TYPE")
}

func mysqlStatementCapabilities(node ast.StmtNode) (bool, bool) {
	kind := strings.TrimSuffix(reflect.TypeOf(node).Elem().Name(), "Stmt")
	rows := false
	write := true
	switch kind {
	case "Select", "SetOpr", "Show", "Explain", "ExplainFor", "Trace", "Table":
		rows = true
		write = false
	case "Call", "Execute":
		rows = true
		write = true
	case "Begin", "Commit", "Rollback", "Set", "Use", "Savepoint", "ReleaseSavepoint", "RollbackToSavepoint":
		write = false
	default:
		// Unknown statements use QueryContext so dynamic result sets are not
		// silently discarded; known DML/DDL use ExecContext for affected rows.
		known := false
		for _, prefix := range []string{"Insert", "Update", "Delete", "Create", "Alter", "Drop", "Truncate", "Rename", "Grant", "Revoke", "Load", "Analyze", "Optimize", "Repair", "Lock", "Unlock", "Kill", "Flush"} {
			if strings.HasPrefix(kind, prefix) {
				known = true
			}
		}
		rows = !known
	}
	if s, ok := node.(*ast.SelectStmt); ok && s.SelectIntoOpt != nil {
		rows = false
		write = true
	}
	return rows, write
}

func pgStatementCapabilities(node *pg.Node) (bool, bool) {
	rows := true
	write := false
	switch {
	case node.GetSelectStmt() != nil:
		if node.GetSelectStmt().IntoClause != nil {
			rows = false
			write = true
		}
	case node.GetInsertStmt() != nil:
		rows = len(node.GetInsertStmt().ReturningList) > 0
		write = true
	case node.GetUpdateStmt() != nil:
		rows = len(node.GetUpdateStmt().ReturningList) > 0
		write = true
	case node.GetDeleteStmt() != nil:
		rows = len(node.GetDeleteStmt().ReturningList) > 0
		write = true
	case node.GetMergeStmt() != nil:
		rows = len(node.GetMergeStmt().ReturningList) > 0
		write = true
	case node.GetCallStmt() != nil:
		rows = true
		write = true
	case node.GetExplainStmt() != nil, node.GetVariableShowStmt() != nil, node.GetFetchStmt() != nil:
		rows = true
	default:
		write = true
		kind := strings.TrimPrefix(reflect.TypeOf(node.Node).String(), "*pg_query.Node_")
		for _, prefix := range []string{"Create", "Alter", "Drop", "Truncate", "Index", "View", "Rule", "Grant", "Vacuum", "Refresh", "Copy", "Do", "Comment", "Reindex", "Cluster"} {
			if strings.HasPrefix(kind, prefix) {
				rows = false
				write = true
			}
		}
		switch kind {
		case "TransactionStmt", "VariableSetStmt", "DiscardStmt", "ClosePortalStmt", "DeallocateStmt", "ListenStmt", "UnlistenStmt", "NotifyStmt":
			rows = false
			write = false
		}
	}
	// A SELECT may wrap data-changing CTEs. Walking the actual protobuf AST
	// detects them without mistaking text inside strings/comments for a write.
	var walk func(protoreflect.Message)
	walk = func(m protoreflect.Message) {
		switch m.Descriptor().Name() {
		case "InsertStmt", "UpdateStmt", "DeleteStmt", "MergeStmt":
			write = true
		}
		m.Range(func(f protoreflect.FieldDescriptor, v protoreflect.Value) bool {
			if f.Kind() != protoreflect.MessageKind {
				return true
			}
			if f.IsList() {
				list := v.List()
				for i := 0; i < list.Len(); i++ {
					walk(list.Get(i).Message())
				}
			} else {
				walk(v.Message())
			}
			return true
		})
	}
	if node != nil {
		walk(node.ProtoReflect())
	}
	return rows, write
}

// TiDB recognizes every versioned comment. Masking only in the parser input
// aligns its behavior with the server version while retaining byte positions.
// Strings and normal comments are skipped by the editor lexer.
func mysqlParserInput(document string, options AnalyzeOptions) (string, error) {
	version := options.MySQLVersion
	if version == 0 {
		version = 80000
	}
	input := []byte(document)
	mode, err := mysql.GetSQLMode(options.MySQLSQLMode)
	if err != nil {
		return "", fmt.Errorf("invalid MySQL SQL mode: %w", err)
	}
	for _, t := range lexMode(document, MySQL, mode.HasANSIQuotesMode(), mode.HasNoBackslashEscapesMode()) {
		if t.kind != 'c' || !strings.HasPrefix(t.text, "/*!") {
			continue
		}
		j := 3
		for j < len(t.text) && t.text[j] >= '0' && t.text[j] <= '9' {
			j++
		}
		if j == 3 {
			continue
		}
		if j-3 != 5 && j-3 != 6 {
			return "", fmt.Errorf("invalid MySQL versioned comment")
		}
		v, _ := strconv.Atoi(t.text[3:j])
		if v <= version {
			// TiDB's lexer consumes exactly five version digits; removing the
			// active condition also supports MySQL's six-digit version format.
			for k := t.start + 3; k < t.start+j; k++ {
				input[k] = ' '
			}
			continue
		}
		for k := t.start; k < t.end; k++ {
			if input[k] != '\n' && input[k] != '\r' {
				input[k] = ' '
			}
		}
	}
	return string(input), nil
}
