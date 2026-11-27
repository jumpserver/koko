package sqlservice

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/go-sql-driver/mysql"
	"github.com/jumpserver/koko/pkg/chen/dialect"
	ob "github.com/oceanbase/go-oceanbase-driver"
)

// Only project documented attributes. Program bodies live exclusively behind
// the independently authorized SHOW CREATE request.
func myObjectAttributes(kind ObjectKind) (table, columns string) {
	switch kind {
	case ProcedureKind, FunctionKind:
		return "ROUTINES", "ROUTINE_TYPE DTD_IDENTIFIER DATA_TYPE DEFINER SECURITY_TYPE IS_DETERMINISTIC SQL_DATA_ACCESS SQL_MODE CREATED LAST_ALTERED ROUTINE_COMMENT CHARACTER_SET_CLIENT COLLATION_CONNECTION DATABASE_COLLATION"
	case TriggerKind:
		return "TRIGGERS", "EVENT_OBJECT_TABLE ACTION_TIMING EVENT_MANIPULATION ACTION_ORIENTATION ACTION_ORDER DEFINER CREATED SQL_MODE CHARACTER_SET_CLIENT COLLATION_CONNECTION DATABASE_COLLATION"
	case EventKind:
		return "EVENTS", "DEFINER TIME_ZONE EVENT_TYPE EXECUTE_AT INTERVAL_VALUE INTERVAL_FIELD STARTS ENDS STATUS ON_COMPLETION CREATED LAST_ALTERED LAST_EXECUTED EVENT_COMMENT SQL_MODE CHARACTER_SET_CLIENT COLLATION_CONNECTION DATABASE_COLLATION"
	}
	return "", ""
}

func myAttributeRows(ctx context.Context, conn *sql.Conn, query string, args ...any) ([]map[string]any, error) {
	rows, err := conn.QueryContext(CatalogQueryContext(ctx), query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	names, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	items := []map[string]any{}
	values := make([]sql.NullString, len(names))
	dest := make([]any, len(names))
	for i := range dest {
		dest[i] = &values[i]
	}
	for rows.Next() {
		if len(items) >= 1024 {
			return nil, fmt.Errorf("catalog object attribute limit exceeded")
		}
		if err := rows.Scan(dest...); err != nil {
			return nil, err
		}
		valuesMap := make(map[string]any, len(names))
		for i, name := range names {
			key := strings.ReplaceAll(strings.ToLower(name), " ", "_")
			if values[i].Valid {
				valuesMap[key] = values[i].String
			} else {
				valuesMap[key] = nil
			}
		}
		items = append(items, valuesMap)
	}
	return items, rows.Err()
}

func myObjectProperties(ctx context.Context, conn *sql.Conn, p MySQLObjectProfile, ref ObjectRef) (map[string]any, error) {
	if !ValidObjectRef(ref) || ref.Schema != ref.Database || len(ref.InputTypes) != 0 {
		return nil, ErrCatalogNotFound
	}
	if len(p.Actions(ref.Kind)) == 0 {
		return nil, ErrCatalogUnsupported
	}
	table, all := myObjectAttributes(ref.Kind)
	var columns []string
	for _, column := range strings.Fields(all) {
		if p.has(table, column) {
			columns = append(columns, column)
		}
	}
	if len(columns) == 0 {
		return nil, ErrCatalogUnsupported
	}
	var where string
	args := []any{ref.Schema, ref.Schema, ref.Name}
	switch ref.Kind {
	case FunctionKind, ProcedureKind:
		where = "ROUTINE_SCHEMA=? AND BINARY ROUTINE_SCHEMA=BINARY ? AND BINARY ROUTINE_NAME=BINARY ? AND ROUTINE_TYPE=?"
		args = append(args, strings.ToUpper(string(ref.Kind)))
	case TriggerKind:
		where = "TRIGGER_SCHEMA=? AND BINARY TRIGGER_SCHEMA=BINARY ? AND BINARY TRIGGER_NAME=BINARY ? AND BINARY EVENT_OBJECT_SCHEMA=BINARY TRIGGER_SCHEMA AND BINARY EVENT_OBJECT_TABLE=BINARY ?"
		args = append(args, ref.Table)
	case EventKind:
		where = "EVENT_SCHEMA=? AND BINARY EVENT_SCHEMA=BINARY ? AND BINARY EVENT_NAME=BINARY ?"
	}
	rows, err := myAttributeRows(ctx, conn, "SELECT "+strings.Join(columns, ",")+" FROM information_schema."+table+" WHERE "+where+" LIMIT 5", args...)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, ErrCatalogNotFound
	}
	props := rows[0]
	if len(rows) > 1 && ref.Kind != TriggerKind {
		return nil, errors.New("ambiguous catalog identity")
	}
	if ref.Kind == TriggerKind {
		events := []string{}
		for _, row := range rows {
			if raw, ok := row["event_manipulation"].(string); ok {
				for _, event := range strings.Split(raw, ",") {
					event = strings.TrimSpace(event)
					if !slices.Contains(events, event) {
						events = append(events, event)
					}
				}
			}
		}
		props["events"] = events
	}
	return props, nil
}

func (p MySQLObjectProfile) defaultsFromDefinition(kind ObjectKind) bool {
	return kind == ProcedureKind && p.Product == dialect.MariaDB && (p.Version.Major > 11 || p.Version.Major == 11 && p.Version.Minor >= 8) && !p.has("PARAMETERS", "PARAMETER_DEFAULT")
}

func MySQLObjectDetail(ctx context.Context, conn *sql.Conn, d dialect.ID, ref ObjectRef) (ObjectDetail, error) {
	result := ObjectDetail{ObjectSummary: ObjectSummary{Ref: ref, Label: ref.Name}}
	if err := catalogScopeAccess(ctx, conn, d, ref.Database, ref.Schema); err != nil {
		return result, err
	}
	p, err := ReadMySQLObjectProfile(CatalogQueryContext(ctx), conn, d)
	if err != nil {
		return result, err
	}
	props, err := myObjectProperties(ctx, conn, p, ref)
	if err != nil {
		return result, err
	}
	result.Properties = props
	for _, a := range p.Actions(ref.Kind) {
		if a != "list" && a != "create" {
			result.Actions = append(result.Actions, a)
		}
	}
	// Visibility is native to each INFORMATION_SCHEMA view. In particular, seeing
	// a routine doesn't guarantee ALTER or EXECUTE with the current active roles.
	props["privilege_check"] = "execution"
	switch ref.Kind {
	case FunctionKind, ProcedureKind:
		if !p.has("PARAMETERS", "SPECIFIC_SCHEMA", "SPECIFIC_NAME", "ROUTINE_TYPE", "ORDINAL_POSITION", "PARAMETER_MODE", "PARAMETER_NAME", "DATA_TYPE", "DTD_IDENTIFIER") {
			props["parameters_available"] = false
			return result, nil
		}
		cols := []string{"ORDINAL_POSITION", "PARAMETER_MODE", "PARAMETER_NAME", "DATA_TYPE", "DTD_IDENTIFIER"}
		for _, col := range []string{"PARAMETER_DEFAULT", "CHARACTER_SET_NAME", "COLLATION_NAME", "CHARACTER_MAXIMUM_LENGTH", "NUMERIC_PRECISION", "NUMERIC_SCALE", "DATETIME_PRECISION"} {
			if p.has("PARAMETERS", col) {
				cols = append(cols, col)
			}
		}
		rows, err := myAttributeRows(ctx, conn, "SELECT "+strings.Join(cols, ",")+` FROM information_schema.PARAMETERS
   WHERE SPECIFIC_SCHEMA=? AND BINARY SPECIFIC_SCHEMA=BINARY ? AND BINARY SPECIFIC_NAME=BINARY ? AND ROUTINE_TYPE=? ORDER BY ORDINAL_POSITION LIMIT 1025`, ref.Schema, ref.Schema, ref.Name, strings.ToUpper(string(ref.Kind)))
		if err != nil {
			return result, err
		}
		props["parameters_available"] = true
		if p.defaultsFromDefinition(ref.Kind) {
			props["defaults_from_definition"] = true
		}
		if ref.Kind == FunctionKind {
			props["return_type"] = props["dtd_identifier"]
		}
		for _, row := range rows {
			ordinal, err := strconv.Atoi(myString(row, "ordinal_position"))
			if err != nil {
				return result, err
			}
			if ordinal == 0 {
				props["return_type"] = row["dtd_identifier"]
				props["return_character_set"] = row["character_set_name"]
				props["return_collation"] = row["collation_name"]
				continue
			}
			par := RoutineParameter{Position: ordinal, Mode: strings.ToUpper(myString(row, "parameter_mode")), Name: myString(row, "parameter_name"), DataType: myString(row, "dtd_identifier"), Type: ObjectTypeRef{Name: myString(row, "data_type")}, CharacterSet: myString(row, "character_set_name"), Collation: myString(row, "collation_name")}
			if def, ok := row["parameter_default"].(string); ok {
				par.HasDefault, par.Default = true, &def
			}
			result.Parameters = append(result.Parameters, par)
		}
	case TriggerKind:
		result.Related = []ObjectSummary{{Ref: ObjectRef{Database: ref.Database, Schema: ref.Schema, Kind: "table", Name: ref.Table}, Label: ref.Table}}
		if p.has("TRIGGERS", "ACTION_ORDER", "ACTION_TIMING", "EVENT_MANIPULATION") {
			if p.Product == dialect.MariaDB && p.Version.Major >= 12 {
				// MariaDB orders each event independently. The integer ACTION_ORDER
				// column exposes only the first event's position. A sibling with a
				// different, overlapping event set makes automatic placement unsafe.
				var matches []string
				args := []any{ref.Schema, ref.Schema, ref.Table, props["action_timing"], props["event_manipulation"]}
				for _, event := range props["events"].([]string) {
					matches = append(matches, "FIND_IN_SET(?,REPLACE(EVENT_MANIPULATION,' ',''))>0")
					args = append(args, event)
				}
				query := `SELECT EXISTS(SELECT 1 FROM information_schema.TRIGGERS
 WHERE TRIGGER_SCHEMA=? AND BINARY TRIGGER_SCHEMA=BINARY ? AND BINARY EVENT_OBJECT_TABLE=BINARY ?
 AND ACTION_TIMING=? AND EVENT_MANIPULATION<>? AND (` + strings.Join(matches, " OR ") + "))"
				var mixed bool
				if err := conn.QueryRowContext(CatalogQueryContext(ctx), query, args...).Scan(&mixed); err != nil {
					return result, err
				}
				if mixed {
					props["trigger_order_requires_choice"] = true
					return result, nil
				}
			}
			// Neighbours are visible to the same TRIGGER grantee on the same table.
			// Keep only their names for recreation instead of enumerating all siblings.
			for _, side := range []struct{ key, op, order string }{{"previous_trigger", "<", "DESC"}, {"next_trigger", ">", "ASC"}} {
				query := `SELECT TRIGGER_NAME FROM information_schema.TRIGGERS WHERE TRIGGER_SCHEMA=? AND BINARY TRIGGER_SCHEMA=BINARY ? AND BINARY EVENT_OBJECT_TABLE=BINARY ?
       AND ACTION_TIMING=? AND EVENT_MANIPULATION=? AND ACTION_ORDER` + side.op + `? ORDER BY ACTION_ORDER ` + side.order + ` LIMIT 1`
				var name string
				e := conn.QueryRowContext(CatalogQueryContext(ctx), query, ref.Schema, ref.Schema, ref.Table, props["action_timing"], props["event_manipulation"], props["action_order"]).Scan(&name)
				if e == nil {
					props[side.key] = name
				} else if !errors.Is(e, sql.ErrNoRows) {
					return result, e
				}
			}
		}
	case EventKind:
		var scheduler string
		e := conn.QueryRowContext(CatalogQueryContext(ctx), "SELECT @@GLOBAL.event_scheduler").Scan(&scheduler)
		if e == nil {
			props["scheduler"] = scheduler
		} else if versionCommentUnavailable(e) {
			props["scheduler"] = nil
		} else {
			return result, e
		}
	}
	return result, nil
}

func myString(m map[string]any, key string) string { s, _ := m[key].(string); return s }

func MySQLDefinitionCommand(d dialect.ID, ref ObjectRef) (string, error) {
	if !d.UsesMySQLSyntax() || !ValidObjectRef(ref) || ref.Schema != ref.Database || len(ref.InputTypes) != 0 {
		return "", ErrCatalogNotFound
	}
	return "SHOW CREATE " + strings.ToUpper(string(ref.Kind)) + " " + quoteIdentifier(ref.Schema, d, '`') + "." + quoteIdentifier(ref.Name, d, '`'), nil
}

func myObjectErrorCode(err error) uint16 {
	var my *mysql.MySQLError
	var ocean *ob.MySQLError
	if errors.As(err, &my) {
		return my.Number
	}
	if errors.As(err, &ocean) {
		return ocean.Number
	}
	return 0
}

func MySQLObjectDefinition(ctx context.Context, conn *sql.Conn, d dialect.ID, ref ObjectRef, authorize func(string) error) (ObjectDefinition, error) {
	return MySQLObjectDefinitionPage(ctx, conn, d, ref, DefinitionPageRequest{}, authorize)
}

func MySQLObjectDefinitionPage(ctx context.Context, conn *sql.Conn, d dialect.ID, ref ObjectRef, page DefinitionPageRequest, authorize func(string) error) (ObjectDefinition, error) {
	if !page.Valid() {
		return ObjectDefinition{}, ErrObjectChanged
	}
	command, err := MySQLDefinitionCommand(d, ref)
	result := ObjectDefinition{Ref: ref, Source: "SHOW CREATE " + strings.ToUpper(string(ref.Kind))}
	if err != nil {
		return result, err
	}
	if err = catalogScopeAccess(ctx, conn, d, ref.Database, ref.Schema); err != nil {
		return result, err
	}
	profile, err := ReadMySQLObjectProfile(CatalogQueryContext(ctx), conn, d)
	if err != nil {
		return result, err
	}
	before, err := myObjectProperties(ctx, conn, profile, ref)
	if err != nil {
		return result, err
	}
	if authorize != nil {
		if err = authorize(command); err != nil {
			return result, err
		}
	}
	rows, err := myAttributeRows(ctx, conn, command)
	if err != nil {
		switch myObjectErrorCode(err) {
		case 1142, 1143, 1227, 1370:
			result.Status = "restricted"
			return result, nil
		case 1305, 1360, 1539:
			return result, ErrCatalogNotFound
		case 1064, 1235:
			result.Status = "unsupported"
			return result, nil
		}
		return result, err
	}
	if len(rows) != 1 {
		return result, ErrCatalogNotFound
	}
	row := rows[0]
	if myString(row, string(ref.Kind)) != ref.Name {
		return result, ErrCatalogNotFound
	}
	field := "create_" + string(ref.Kind)
	if ref.Kind == TriggerKind {
		if _, ok := row["sql_original_statement"]; ok {
			field = "sql_original_statement"
		}
	}
	text, available := row[field]
	if !available {
		result.Status = "unsupported"
		return result, nil
	}
	result.Text, _ = text.(string)
	if result.Text == "" {
		result.Status = "restricted"
		return result, nil
	}
	// Re-check the complete logical identity after SHOW, which addresses a trigger
	// only by schema/name. No unverified definition is returned on a concurrent change.
	after, err := myObjectProperties(ctx, conn, profile, ref)
	if err != nil {
		return ObjectDefinition{}, err
	}
	delete(before, "last_executed")
	delete(after, "last_executed")
	original, _ := json.Marshal(before)
	current, _ := json.Marshal(after)
	if string(original) != string(current) {
		return ObjectDefinition{}, ErrObjectChanged
	}
	result.Environment = map[string]string{}
	for _, k := range []string{"sql_mode", "character_set_client", "collation_connection", "database_collation", "time_zone"} {
		source := row
		if profile.Product == dialect.OceanBaseMySQL && ref.Kind == TriggerKind && (k == "collation_connection" || k == "database_collation") {
			// SHOW CREATE TRIGGER reports character set names in its collation
			// fields. The trigger's checked dictionary row has the native collations.
			source = after
		}
		if s, ok := source[k].(string); ok {
			result.Environment[k] = s
		}
	}
	overLimit := DocumentExceedsLimit(result.Text)
	versionText := result.Text
	if overLimit {
		// SHOW CREATE returns one native field. Hash it before slicing so a
		// change anywhere in the full source invalidates every page.
		versionText = definitionFingerprint(result.Text)
	}
	evidence, err := json.Marshal([]any{ref, versionText, result.Environment, after})
	if err != nil {
		return ObjectDefinition{}, err
	}
	result.Version = definitionFingerprint(string(evidence))
	if page.Version != "" && page.Version != result.Version {
		return ObjectDefinition{}, ErrObjectChanged
	}
	if overLimit {
		total := utf8.RuneCountInString(result.Text)
		if page.Offset >= total {
			return ObjectDefinition{}, ErrObjectChanged
		}
		body := result.Text
		for i := 0; i < page.Offset; i++ {
			_, n := utf8.DecodeRuneInString(body)
			body = body[n:]
		}
		if err := result.setPage(page, body, total); err != nil {
			return ObjectDefinition{}, err
		}
		return result, nil
	}
	if page.Offset != 0 {
		return ObjectDefinition{}, ErrObjectChanged
	}
	result.Status = "complete"
	// MariaDB 11.8 introduced parameter defaults before the PARAMETERS view
	// exposed them. Derive only their positions after the SHOW CREATE ACL check.
	if profile.defaultsFromDefinition(ref.Kind) {
		defaults, e := mysqlRoutineDefaultPositions(result.Text, ref, AnalyzeOptions{MySQLSQLMode: result.Environment["sql_mode"]})
		if e == nil {
			for pos := range defaults {
				result.DefaultPositions = append(result.DefaultPositions, pos)
			}
			slices.Sort(result.DefaultPositions)
		}
	}
	return result, nil
}
