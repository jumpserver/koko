package sqlservice

import (
	"errors"
	"strings"

	"github.com/jumpserver/koko/pkg/chen/dialect"
	"github.com/jumpserver/koko/pkg/chen/sqlanalysis"
	pg "github.com/pganalyze/pg_query_go/v6"
	"github.com/pingcap/tidb/pkg/parser"
	"github.com/pingcap/tidb/pkg/parser/ast"
	"github.com/pingcap/tidb/pkg/parser/mysql"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// ClickHouseJSONPlanPrefix pins the vendor PLAN format without executing ANALYZE.
const ClickHouseJSONPlanPrefix = "EXPLAIN PLAN json=1, description=1, actions=0 "

// ErrExplainShowNotSupported reports a metadata SHOW statement in plan mode.
var ErrExplainShowNotSupported = errors.New("explain_show_not_supported")

// EstimatedPlanSQL validates the original statement using the physical console's
// parser options. It is an admission check, never an authorization or purity proof.
func EstimatedPlanSQL(d dialect.ID, document string, options AnalyzeOptions) (string, error) {
	switch d {
	case dialect.PostgreSQL, dialect.MySQL, dialect.MariaDB, dialect.OceanBaseMySQL, dialect.ClickHouse, dialect.Oracle, dialect.SQLServer, dialect.DB2, dialect.Dameng, dialect.OceanBaseOracle:
	default:
		return "", errors.New("explain_not_supported")
	}
	a, err := AnalyzeWithOptions(d, document, options)
	if err != nil || len(a.Statements) != 1 {
		return "", errors.New("explain_requires_single_select")
	}
	s := a.Statements[0]
	if d.UsesMySQLSyntax() && s.Kind == "show" {
		return "", ErrExplainShowNotSupported
	}
	vendor := d == dialect.Oracle || d == dialect.SQLServer || d == dialect.DB2 || d == dialect.Dameng || d == dialect.OceanBaseOracle
	if (s.MayWrite && !vendor) || !s.ReturnsRows || (s.Kind != "select" && s.Kind != "setopr") {
		return "", errors.New("explain_requires_single_select")
	}
	if vendor {
		// Native adapters mark every routine call as MayWrite until the consumer
		// checks its own planning contract. Never change their shared analysis facts.
		if s.Facts != nil {
			for _, f := range s.Facts.Features {
				switch f {
				case sqlanalysis.Locking, sqlanalysis.Sequence, sqlanalysis.SelectInto, sqlanalysis.OutputInto, sqlanalysis.Assignment, sqlanalysis.Returning:
					return "", errors.New("explain_unsafe_select")
				}
			}
		}

		if _, err := planReadDocument(d, document, options); err != nil {
			return "", errors.New("explain_unsafe_select")
		}
		if err := validateVendorPlanFunctions(d, document); err != nil {
			return "", err
		}
		switch d {
		case dialect.Oracle, dialect.DB2:
			return "EXPLAIN PLAN FOR " + strings.TrimSpace(s.SQL), nil
		case dialect.SQLServer:
			// SHOWPLAN is scoped by the server executor to this one admitted query.
			return strings.TrimSpace(s.SQL), nil
		default:
			return "EXPLAIN " + strings.TrimSpace(s.SQL), nil
		}
	}
	prefix := "EXPLAIN "
	switch {
	case d == dialect.PostgreSQL:
		tree, err := pg.Parse(document)
		if err != nil {
			return "", errors.New("explain_requires_single_select")
		}
		unsafe := false
		var visit func(protoreflect.Message)
		visit = func(m protoreflect.Message) {
			// Include nested SELECT INTO and locking clauses, not just the outer SELECT.
			if m.Descriptor().Name() == "IntoClause" || m.Descriptor().Name() == "LockingClause" {
				unsafe = true
			}
			m.Range(func(f protoreflect.FieldDescriptor, v protoreflect.Value) bool {
				if f.Kind() == protoreflect.MessageKind {
					if f.IsList() {
						for i := 0; i < v.List().Len(); i++ {
							visit(v.List().Get(i).Message())
						}
					} else {
						visit(v.Message())
					}
				}
				return !unsafe
			})
		}
		visit(tree.ProtoReflect())
		if unsafe {
			return "", errors.New("explain_unsafe_select")
		}
		prefix = "EXPLAIN (FORMAT JSON, ANALYZE FALSE) "
	case d.UsesMySQLSyntax():
		input, err := mysqlParserInput(document, options)
		if err != nil {
			return "", errors.New("explain_requires_single_select")
		}
		p := parser.New()
		mode, err := mysql.GetSQLMode(options.MySQLSQLMode)
		if err != nil {
			return "", errors.New("explain_requires_single_select")
		}
		p.SetSQLMode(mode)
		nodes, _, err := p.ParseSQL(input)
		if err == nil {
			guard := &planSelectGuard{}
			for _, n := range nodes {
				n.Accept(guard)
			}
			if guard.unsafe {
				return "", errors.New("explain_unsafe_select")
			}
		} else if d == dialect.MySQL {
			return "", errors.New("explain_requires_single_select")
		} else if _, err := planReadDocument(d, document, options); err != nil {
			return "", errors.New("explain_unsafe_select")
		}
		// Pin classic output independently of MySQL's session explain_format setting.
		if d == dialect.MySQL {
			prefix = "EXPLAIN FORMAT=TRADITIONAL "
		}
	default:
		if _, err := planReadDocument(d, document, options); err != nil {
			return "", errors.New("explain_unsafe_select")
		}
		prefix = "EXPLAIN PLAN "
	}
	return prefix + strings.TrimSpace(s.SQL), nil
}

type planSelectGuard struct{ unsafe bool }

func (g *planSelectGuard) Enter(n ast.Node) (ast.Node, bool) {
	if s, ok := n.(*ast.SelectStmt); ok && (s.SelectIntoOpt != nil || s.LockInfo != nil && s.LockInfo.LockType != ast.SelectLockNone) {
		g.unsafe = true
	}
	return n, g.unsafe
}
func (g *planSelectGuard) Leave(n ast.Node) (ast.Node, bool) { return n, !g.unsafe }

// Scanner dialects have no native locking AST; reject FOR SHARE as well as
// the write/INTO/locking forms already covered by analyzeSelectDocument.
func planReadDocument(d dialect.ID, document string, options AnalyzeOptions) (Analysis, error) {
	a, err := analyzeSelectDocument(d, document, options)
	if err != nil {
		return a, err
	}
	mode, _ := mysql.GetSQLMode(options.MySQLSQLMode)
	previousFor := false
	for _, token := range lexMode(document, d, mode.HasANSIQuotesMode(), mode.HasNoBackslashEscapesMode()) {
		if token.kind == 'c' {
			continue
		}
		if previousFor && token.is("SHARE") {
			return a, errors.New("explain_unsafe_select")
		}
		previousFor = token.is("FOR")
	}
	return a, nil
}

// Restrict planning-time calls for dialects whose optimizer may invoke routines.
// This lexer operates only after single-read-statement admission. It is not a
// proof that SELECT is free of effects; the database's own privileges still apply.
func validateVendorPlanFunctions(d dialect.ID, document string) error {
	allowed := ""
	switch d {
	case dialect.Oracle:
		allowed = " abs acos add_months ascii asin atan atan2 avg bitand cast ceil chr coalesce concat convert corr cos cosh count covar_pop covar_samp decode dense_rank dump exp extract first_value floor greatest instr instrb json_array json_arrayagg json_exists json_object json_objectagg json_query json_value lag last_day last_value lead least length lengthb listagg ln log lower lpad ltrim max median min mod months_between next_day nth_value ntile nullif nvl nvl2 ora_hash percent_rank percentile_cont percentile_disc power rank regexp_count regexp_instr regexp_like regexp_replace regexp_substr replace round row_number rpad rtrim sign sin sinh sqrt standard_hash stddev stddev_pop stddev_samp substr substrb sum tan tanh to_binary_double to_binary_float to_char to_clob to_date to_number to_timestamp to_timestamp_tz translate trim trunc upper variance xmlagg xmlelement xmlforest xmlserialize "
	case dialect.SQLServer:
		allowed = " abs acos asin atan atn2 avg ceiling count count_big degrees exp floor log log10 max min pi power radians rand round sign sin sqrt square sum tan checksum binary_checksum cast convert try_cast try_convert parse try_parse coalesce isnull nullif iif choose case current_timestamp getdate getutcdate sysdatetime sysutcdatetime sysdatetimeoffset dateadd datediff datediff_big datefromparts datetimefromparts datetime2fromparts datetimeoffsetfromparts datename datepart day eomonth isdate month smalldatetimefromparts timefromparts year char concat concat_ws difference format left len lower ltrim nchar patindex quotename replace replicate reverse right rtrim soundex space str string_agg string_escape string_split stuff substring translate trim unicode upper json_query json_value isjson json_modify row_number rank dense_rank ntile lag lead first_value last_value percent_rank percentile_cont percentile_disc cume_dist newid newsequentialid host_name db_name schema_name object_name type_name user_name suser_sname original_login has_perms_by_name xact_state grouping grouping_id "
	case dialect.DB2:
		allowed = " abs acos asin atan atan2 avg bigint ceil ceiling char coalesce concat cos count date day days decimal decode degrees dense_rank double exp extract first_value float floor greatest hex hour integer lag last_day last_value lead least length ln locate log log10 lower lpad ltrim max microsecond midnight_seconds min minute mod month months_between nullif nth_value ntile power quarter radians rank regexp_count regexp_instr regexp_like regexp_replace regexp_substr replace round row_number rpad rtrim second sign sin smallint sqrt stddev stddev_samp substring sum tan time timestamp translate trim trunc truncate upper variance var_samp varchar week year "
	case dialect.Dameng:
		allowed = " abs avg cast ceil ceiling coalesce concat count current_date current_time current_timestamp dateadd datediff dense_rank extract floor greatest lag lead least length lower ltrim max min mod nullif rank replace round row_number rtrim substr substring sum trim trunc upper "
	case dialect.OceanBaseOracle:
		allowed = " abs acos add_months ascii asin atan atan2 avg bitand cast ceil chr coalesce concat convert corr cos cosh count covar_pop covar_samp decode dense_rank dump exp extract first_value floor greatest instr instrb json_array json_arrayagg json_exists json_object json_objectagg json_query json_value lag last_day last_value lead least length lengthb listagg ln log lower lpad ltrim max median min mod months_between next_day nth_value ntile nullif nvl nvl2 ora_hash percent_rank percentile_cont percentile_disc power rank regexp_count regexp_instr regexp_like regexp_replace regexp_substr replace round row_number rpad rtrim sign sin sinh sqrt standard_hash stddev stddev_pop stddev_samp substr substrb sum tan tanh to_binary_double to_binary_float to_char to_clob to_date to_number to_timestamp to_timestamp_tz translate trim trunc upper variance xmlagg xmlelement xmlforest xmlserialize "
	}
	grammar := " select with as from where in exists over partition order group by having values top on join using rollup cube grouping sets within offset fetch case when then else end char varchar varchar2 nvarchar nvarchar2 nchar number numeric decimal timestamp time day second interval option index forceseek "
	var ts []token
	for _, t := range lexMode(document, d, false, false) {
		if t.kind == 'c' {
			continue
		}
		ts = append(ts, t)
	}
	hintUntil := -1
	for i, t := range ts {
		if i <= hintUntil {
			continue
		}
		if d == dialect.SQLServer && t.is("WITH") && i+1 < len(ts) && ts[i+1].kind == '(' {
			depth := 0
			for j := i + 1; j < len(ts); j++ {
				if ts[j].kind == '(' {
					depth++
				}
				if ts[j].kind == ')' {
					depth--
					if depth == 0 {
						hintUntil = j
						break
					}
				}
			}
			continue
		}
		if !t.quoted && t.kind == 'i' {
			switch strings.ToUpper(t.value) {
			case "UPDLOCK", "XLOCK", "HOLDLOCK", "TABLOCKX", "READPAST":
				return errors.New("explain_unsafe_select")
			}
		}
		if !t.ident() || i+1 >= len(ts) || ts[i+1].kind != '(' {
			continue
		}
		name := strings.ToLower(t.value)

		// CTE column lists are not routine calls.
		close := i + 1
		depth := 0
		for ; close < len(ts); close++ {
			if ts[close].kind == '(' {
				depth++
			}
			if ts[close].kind == ')' {
				depth--
				if depth == 0 {
					break
				}
			}
		}
		if close+2 < len(ts) && ts[close+1].is("AS") && ts[close+2].kind == '(' {
			continue
		}
		if i > 0 && ts[i-1].kind == '.' {
			schema := ""
			if i >= 2 {
				schema = strings.ToLower(ts[i-2].value)
			}
			allowedSchema := schema == "sys" && (d == dialect.Oracle || d == dialect.SQLServer || d == dialect.OceanBaseOracle) || schema == "standard" && (d == dialect.Oracle || d == dialect.OceanBaseOracle) || d == dialect.DB2 && strings.Contains(" sysibm sysfun sysproc sysibmadm ", " "+schema+" ")
			if !allowedSchema || i >= 3 && ts[i-3].kind == '.' {
				return errors.New("explain_unsafe_function")
			}
		}

		if t.quoted || !strings.Contains(allowed, " "+name+" ") && !strings.Contains(grammar, " "+name+" ") {
			return errors.New("explain_unsafe_function")
		}
	}
	return nil
}
