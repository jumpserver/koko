package sqlservice

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/jumpserver/koko/pkg/chen/dialect"
)

const EventKind ObjectKind = "event"

type MySQLObjectProfile struct {
	Product  dialect.ID
	Version  dialect.Version
	Contract string
	fields   map[string]bool
	eventDDL bool
}

// OceanBase embeds its own release after the MySQL compatibility version.
var oceanBaseEventBP = regexp.MustCompile(`(?i)OceanBase[^\s]*(?:-v|\s+v?)4\.[23]\.5\.(\d+)(?:[-.\s]|$)`)

func oceanBaseEventDDL(v dialect.Version, banner string) bool {
	if v.Major != 4 {
		return false
	}
	if v.Minor >= 4 {
		return v.Minor > 4 || v.Patch >= 2
	}
	if (v.Minor != 2 && v.Minor != 3) || v.Patch < 5 {
		return false
	}
	if v.Patch > 5 {
		return true
	}
	m := oceanBaseEventBP.FindStringSubmatch(banner)
	if m == nil {
		return false
	}
	bp, err := strconv.Atoi(m[1])
	return err == nil && bp >= 2
}

// CURRENT_ROLE is native to MySQL 8, MariaDB and the OceanBase MySQL
// role-management releases. Capture it on the same connection as the plan.
func (p MySQLObjectProfile) supportsCurrentRole() bool {
	switch p.Product {
	case dialect.MySQL:
		return p.Version.Major >= 8
	case dialect.MariaDB:
		return true
	case dialect.OceanBaseMySQL:
		v := p.Version
		return v.Major == 4 && (v.Minor > 3 || v.Minor == 3 && v.Patch >= 1 || v.Minor == 2 && v.Patch >= 3)
	}
	return false
}

func (p MySQLObjectProfile) has(table string, cols ...string) bool {
	for _, column := range cols {
		if !p.fields[table+"."+column] {
			return false
		}
	}
	return true
}

func ReadMySQLObjectProfile(ctx context.Context, conn *sql.Conn, d dialect.ID) (MySQLObjectProfile, error) {
	p := MySQLObjectProfile{Product: d}
	if !d.UsesMySQLSyntax() {
		return p, ErrCatalogUnsupported
	}
	ctx = CatalogQueryContext(ctx)
	banner, err := ReadMySQLServerVersion(ctx, conn, d)
	if err = RequireReportedVersion(ctx, err); err != nil {
		return p, err
	}
	var ok bool
	switch {
	case d == dialect.OceanBaseMySQL || banner.ReportsOceanBase():
		if !banner.ReportsOceanBase() {
			return p, nil
		}
		var mode string
		if err = conn.QueryRowContext(ctx, "SELECT @@ob_compatibility_mode").Scan(&mode); err != nil {
			return p, err
		}
		if !strings.EqualFold(mode, "MYSQL") {
			return p, nil
		}
		p.Product = dialect.OceanBaseMySQL
		p.Version, ok = dialect.ParseOceanBaseVersion(banner.Product)
		p.eventDDL = oceanBaseEventDDL(p.Version, banner.Product)
	case d == dialect.MariaDB || banner.ReportsMariaDB():
		p.Product = dialect.MariaDB
		p.Version, _, ok = dialect.ParseMariaDBVersion(banner.Product)
	case d == dialect.MySQL:
		p.Version, _, ok = dialect.ParseNumericVersion(banner.Reported)
	}
	if !ok {
		return p, nil
	}
	rows, err := conn.QueryContext(ctx, `SELECT TABLE_NAME,COLUMN_NAME FROM information_schema.COLUMNS
 WHERE TABLE_SCHEMA='information_schema' AND TABLE_NAME IN ('ROUTINES','PARAMETERS','TRIGGERS','EVENTS')
 ORDER BY TABLE_NAME,ORDINAL_POSITION LIMIT 257`)
	if err != nil {
		return p, err
	}
	var fields []string
	p.fields, fields, err = readMySQLCatalogFields(rows, 256, true)
	if err != nil {
		return p, err
	}
	p.Contract = definitionFingerprint(string(p.Product) + "\x00" + banner.Product + "\x00" + strings.Join(fields, ","))
	return p, nil
}

func (p MySQLObjectProfile) Actions(kind ObjectKind) []string {
	var readable bool
	switch kind {
	case FunctionKind, ProcedureKind:
		readable = p.has("ROUTINES", "ROUTINE_SCHEMA", "ROUTINE_NAME", "ROUTINE_TYPE")
	case TriggerKind:
		readable = p.has("TRIGGERS", "TRIGGER_SCHEMA", "TRIGGER_NAME", "EVENT_OBJECT_SCHEMA", "EVENT_OBJECT_TABLE")
	case EventKind:
		readable = p.has("EVENTS", "EVENT_SCHEMA", "EVENT_NAME")
	}
	if !readable {
		return nil
	}
	actions := []string{"list", "inspect", "definition"}
	// An OB compatibility dictionary can predate its DDL support. Gate writes
	// on the product release, independently of visibility of the view.
	if p.Product == dialect.OceanBaseMySQL {
		if p.Version.Major != 4 {
			return actions
		}
		if p.Version.Minor < 2 || p.Version.Minor == 2 && p.Version.Patch < 1 || kind == EventKind && !p.eventDDL {
			return actions
		}
	}
	actions = append(actions, "create", "edit", "drop")
	switch kind {
	case ProcedureKind, FunctionKind:
		if p.has("PARAMETERS", "SPECIFIC_SCHEMA", "SPECIFIC_NAME", "ROUTINE_TYPE", "ORDINAL_POSITION", "PARAMETER_MODE", "PARAMETER_NAME", "DATA_TYPE", "DTD_IDENTIFIER") {
			actions = append(actions, "invoke")
		}
	case EventKind:
		actions = append(actions, "setState", "rename")
	}
	return actions
}

func (p MySQLObjectProfile) addCollections(result *CatalogCapabilities) {
	all := []ObjectKind{"table", "view"}
	for _, g := range []struct {
		name  string
		kinds []ObjectKind
	}{{"routine", []ObjectKind{FunctionKind, ProcedureKind}}, {"trigger", []ObjectKind{TriggerKind}}, {"event", []ObjectKind{EventKind}}} {
		actions := p.Actions(g.kinds[0])
		if len(actions) == 0 {
			continue
		}
		result.Collections[g.name] = CatalogCollection{Kinds: g.kinds, Actions: actions}
		all = append(all, g.kinds...)
	}
	if len(all) > 2 {
		result.Collections["all"] = CatalogCollection{Kinds: all, Actions: []string{"list"}}
	}
}

var myObjectKinds = []ObjectKind{"", "table", "view", FunctionKind, ProcedureKind, TriggerKind, EventKind}

type MySQLObjectListRequest struct {
	ObjectSearchRequest
	Collection string
	Kind       ObjectKind
	Table      string
}

func MySQLObjectRows(ctx context.Context, conn *sql.Conn, d dialect.ID, p MySQLObjectProfile, r MySQLObjectListRequest) ([]ObjectSummary, error) {
	if p.Contract == "" {
		return nil, ErrCatalogUnsupported
	}
	if err := validateCatalogInput(conn, d, r.Database, r.Schema, r.Query, r.Table, r.After.Ref.Name, r.After.Ref.Table); err != nil {
		return nil, err
	}
	if err := catalogScopeAccess(ctx, conn, d, r.Database, r.Schema); err != nil {
		return nil, err
	}
	after := slices.Index(myObjectKinds, r.After.Ref.Kind)
	if after < 0 {
		return nil, ErrCatalogNotFound
	}
	var sources []string
	var args []any
	pageSize := catalogLimit(r.Limit) + 1
	// Each source applies its privileges, name filter and continuation before
	// contributing at most one page+lookahead to the bounded UNION.
	add := func(source string, values ...any) {
		sqlText := `SELECT * FROM (SELECT * FROM (` + source + `) ids WHERE object_name LIKE ? ESCAPE '!'
    AND (BINARY object_name,kind_rank,BINARY parent_name)>(BINARY ?,?,BINARY ?)
    ORDER BY BINARY object_name,kind_rank,BINARY parent_name LIMIT ?) part`
		sources = append(sources, sqlText)
		args = append(args, values...)
		args = append(args, catalogSearch(r.Query), r.After.Ref.Name, after, r.After.Ref.Table, pageSize)
	}
	if r.Collection == "all" {
		add(`SELECT TABLE_NAME AS object_name,CASE TABLE_TYPE WHEN 'VIEW' THEN 2 ELSE 1 END AS kind_rank,'' AS parent_name,
    CASE TABLE_TYPE WHEN 'VIEW' THEN 'view' ELSE 'table' END AS subtype FROM information_schema.TABLES
    WHERE TABLE_SCHEMA=? AND BINARY TABLE_SCHEMA=BINARY ? AND TABLE_TYPE IN ('BASE TABLE','VIEW','SYSTEM VIEW','SYSTEM VERSIONED')`, r.Database, r.Database)
	}
	if (r.Collection == "routine" || r.Collection == "all") && len(p.Actions(FunctionKind)) > 0 {
		src := `SELECT ROUTINE_NAME AS object_name,CASE ROUTINE_TYPE WHEN 'PROCEDURE' THEN 4 ELSE 3 END AS kind_rank,'' AS parent_name,'' AS subtype
    FROM information_schema.ROUTINES WHERE ROUTINE_SCHEMA=? AND BINARY ROUTINE_SCHEMA=BINARY ? AND ROUTINE_TYPE IN ('FUNCTION','PROCEDURE')`
		if r.Kind != "" {
			add(src+" AND ROUTINE_TYPE=?", r.Database, r.Database, strings.ToUpper(string(r.Kind)))
		} else {
			add(src, r.Database, r.Database)
		}
	}
	if (r.Collection == "trigger" || r.Collection == "all") && len(p.Actions(TriggerKind)) > 0 {
		src := `SELECT DISTINCT TRIGGER_NAME AS object_name,5 AS kind_rank,EVENT_OBJECT_TABLE AS parent_name,'' AS subtype
   FROM information_schema.TRIGGERS WHERE TRIGGER_SCHEMA=? AND BINARY TRIGGER_SCHEMA=BINARY ? AND BINARY EVENT_OBJECT_SCHEMA=BINARY TRIGGER_SCHEMA`
		if r.Table != "" {
			add(src+" AND BINARY EVENT_OBJECT_TABLE=BINARY ?", r.Database, r.Database, r.Table)
		} else {
			add(src, r.Database, r.Database)
		}
	}
	if (r.Collection == "event" || r.Collection == "all") && len(p.Actions(EventKind)) > 0 {
		add(`SELECT EVENT_NAME AS object_name,6 AS kind_rank,'' AS parent_name,'' AS subtype
    FROM information_schema.EVENTS WHERE EVENT_SCHEMA=? AND BINARY EVENT_SCHEMA=BINARY ?`, r.Database, r.Database)
	}
	if len(sources) == 0 {
		return nil, ErrCatalogUnsupported
	}
	query := "SELECT * FROM (" + strings.Join(sources, " UNION ALL ") + ") results ORDER BY BINARY object_name,kind_rank,BINARY parent_name LIMIT ?"
	args = append(args, pageSize)
	rows, err := conn.QueryContext(CatalogQueryContext(ctx), query, args...)
	if err != nil {
		return nil, fmt.Errorf("list catalog program objects: %w", err)
	}
	defer rows.Close()
	items := []ObjectSummary{}
	for rows.Next() {
		var item ObjectSummary
		var rank int
		if err := rows.Scan(&item.Ref.Name, &rank, &item.Ref.Table, &item.ObjectType); err != nil {
			return nil, err
		}
		item.Ref.Database, item.Ref.Schema, item.Ref.Kind, item.Label = r.Database, r.Database, myObjectKinds[rank], item.Ref.Name
		items = append(items, item)
	}
	return items, rows.Err()
}
