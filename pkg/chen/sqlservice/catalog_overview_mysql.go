package sqlservice

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"

	mysql "github.com/go-sql-driver/mysql"
	"github.com/jumpserver/koko/pkg/chen/dialect"
	ob "github.com/oceanbase/go-oceanbase-driver"
)

var ErrOverviewContractChanged = errors.New("overview product, version or catalog fields changed; refresh the overview")

// No global/session capability cache: every page probes the same authorized
// catalog connection used for its data. Failure stops the request, without
// trying a query containing unverified columns or claiming an empty catalog.
type overviewMyCapabilities struct {
	dialect  dialect.ID
	version  MySQLServerVersion
	contract string
	columns  map[string]bool
	storage  bool
}

func probeOverviewMyCapabilities(ctx context.Context, conn *sql.Conn, d dialect.ID, section string) (*overviewMyCapabilities, error) {
	version, err := ReadMySQLServerVersion(ctx, conn, d)
	if err != nil {
		return nil, err
	}
	c := &overviewMyCapabilities{dialect: d, version: version, columns: map[string]bool{}, storage: true}
	if len(version.Product) > 256 || !validOverviewText(version.Product) {
		return nil, errors.New("invalid overview server version")
	}
	switch {
	case d == dialect.MySQL && version.ReportsOceanBase():
		// A MySQL-protocol asset may use OceanBase. Its server version identifies
		// which storage and index capabilities apply to the shared catalog SQL.
		c.dialect = dialect.OceanBaseMySQL
	case d == dialect.MariaDB && !version.ReportsMariaDB(), d == dialect.OceanBaseMySQL && !version.ReportsOceanBase(), d == dialect.MySQL && version.ReportsMariaDB():
		return nil, ErrOverviewContractChanged
	}
	if c.dialect == dialect.OceanBaseMySQL {
		c.storage = overviewOBStorageVersion(version.Product)
	}
	table := "TABLES"
	required := []string{"TABLE_SCHEMA", "TABLE_NAME", "TABLE_TYPE"}
	if section == "indexes" {
		table = "STATISTICS"
		required = []string{"TABLE_SCHEMA", "TABLE_NAME", "INDEX_NAME", "SEQ_IN_INDEX"}
	}
	if section == "properties" {
		table = "SCHEMATA"
		required = []string{"SCHEMA_NAME"}
	}
	rows, err := conn.QueryContext(ctx, `SELECT COLUMN_NAME FROM information_schema.COLUMNS WHERE TABLE_SCHEMA='information_schema' AND TABLE_NAME=? ORDER BY ORDINAL_POSITION LIMIT 129`, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	names := []string{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		if len(names) >= 128 || !utf8.ValidString(name) || utf8.RuneCountInString(name) > 128 {
			return nil, errors.New("overview capability budget exceeded")
		}
		name = strings.ToUpper(name)
		c.columns[name] = true
		names = append(names, name)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for _, name := range required {
		if !c.columns[name] {
			return nil, errors.New("overview catalog capability unavailable")
		}
	}
	sort.Strings(names)
	c.contract = fmt.Sprintf("%x", sha256.Sum256([]byte(string(c.dialect)+"\x00"+version.Product+"\x00"+table+"\x00"+strings.Join(names, ","))))
	return c, nil
}

func overviewOBStorageVersion(version string) bool {
	v, ok := dialect.ParseOceanBaseVersion(version)
	// Official TABLES contract: 4.2.4+ and 4.3.5+ expose macroblock
	// storage sizes; older compatibility placeholders are not measured zeroes.
	return ok && v.Major == 4 && (v.Minor == 2 && v.Patch >= 4 || v.Minor == 3 && v.Patch >= 5 || v.Minor >= 4)
}

func (c *overviewMyCapabilities) state(key string) string {
	column := map[string]string{"estimated_rows": "TABLE_ROWS", "data_bytes": "DATA_LENGTH", "index_bytes": "INDEX_LENGTH", "engine": "ENGINE", "collation": "TABLE_COLLATION", "column": "COLUMN_NAME", "expression": "EXPRESSION", "unique": "NON_UNIQUE", "method": "INDEX_TYPE", "position": "SEQ_IN_INDEX", "direction": "COLLATION", "prefix_length": "SUB_PART", "comment": "TABLE_COMMENT", "created": "CREATE_TIME", "average_row_length": "AVG_ROW_LENGTH"}[key]
	if key == "definition" || key == "last_analyzed" {
		if key == "last_analyzed" {
			return "unknown"
		}
		return "unsupported"
	}
	if key == "character_set" {
		if c.columns["SCHEMA_NAME"] {
			column = "DEFAULT_CHARACTER_SET_NAME"
		} else {
			column = "TABLE_COLLATION"
		}
	}
	if key == "collation" && c.columns["SCHEMA_NAME"] {
		column = "DEFAULT_COLLATION_NAME"
	}
	if key == "expression" && c.dialect != dialect.MySQL {
		return "unsupported"
	}
	if key == "total_bytes" {
		if !c.columns["DATA_LENGTH"] || !c.columns["INDEX_LENGTH"] {
			return "unsupported"
		}
	} else if column != "" && !c.columns[column] {
		return "unsupported"
	}
	if strings.HasSuffix(key, "_bytes") && !c.storage {
		return "unknown"
	}
	return "unknown"
}
func (c *overviewMyCapabilities) value(key string, value sql.NullString, section string) OverviewValue {
	source := overviewSource(c.dialect, section, key)
	state := c.state(key)
	if state == "unsupported" {
		source += "; field not provided by this product/catalog"
		value = sql.NullString{}
	}
	if (strings.HasSuffix(key, "_bytes") || key == "average_row_length") && !c.storage {
		source += "; storage semantics unverified for this OceanBase version; compatibility values omitted"
		value = sql.NullString{}
	}
	return overviewValue(value, state, source)
}

func (c *overviewMyCapabilities) adaptQuery(query catalogQuery, section string) catalogQuery {
	// Replacements are fixed catalog identifiers, never request text. Identity
	// and ordering columns were required by the probe; optional facts become NULL.
	pairs := []string{}
	for _, name := range []string{"TABLE_ROWS", "DATA_LENGTH", "INDEX_LENGTH", "ENGINE", "TABLE_COLLATION", "TABLE_COMMENT", "CREATE_TIME", "AVG_ROW_LENGTH"} {
		if !c.columns[name] || !c.storage && (name == "DATA_LENGTH" || name == "INDEX_LENGTH") {
			pairs = append(pairs, "t."+name, "NULL")
		}
	}
	for _, name := range []string{"NON_UNIQUE", "INDEX_TYPE", "COLUMN_NAME", "EXPRESSION", "COLLATION", "SUB_PART"} {
		if !c.columns[name] || name == "EXPRESSION" && c.dialect != dialect.MySQL {
			pairs = append(pairs, "s."+name, "NULL")
		}
	}
	for _, name := range []string{"DEFAULT_CHARACTER_SET_NAME", "DEFAULT_COLLATION_NAME"} {
		if !c.columns[name] {
			pairs = append(pairs, name, "NULL")
		}
	}
	query.SQL = strings.NewReplacer(pairs...).Replace(query.SQL)
	if c.dialect == dialect.MariaDB {
		query.SQL = strings.ReplaceAll(query.SQL, "t.TABLE_TYPE IN ('BASE TABLE','VIEW')", "t.TABLE_TYPE IN ('BASE TABLE','SYSTEM VERSIONED','VIEW')")
		query.SQL = strings.ReplaceAll(query.SQL, "t.TABLE_TYPE='BASE TABLE'", "t.TABLE_TYPE IN ('BASE TABLE','SYSTEM VERSIONED')")
		query.SQL = strings.ReplaceAll(query.SQL, "t.TABLE_TYPE=?", "(t.TABLE_TYPE=? OR (?='BASE TABLE' AND t.TABLE_TYPE='SYSTEM VERSIONED'))")
		if section == "tables" || section == "views" {
			// Bind the table kind twice for the system-versioned table predicate.
			query.Args = append([]any{query.Args[0], query.Args[1], query.Args[1]}, query.Args[2:]...)
		}
	}
	return query
}

func overviewDatabaseDDL(ctx context.Context, conn *sql.Conn, d dialect.ID, database string) (OverviewValue, error) {
	source := string(d) + "; SHOW CREATE DATABASE; database options only, excludes table definitions and data"
	// database is the already-authorized fixed scope. Backtick escaping remains
	// valid under ANSI_QUOTES and NO_BACKSLASH_ESCAPES; it is not a SQL literal.
	rows, err := conn.QueryContext(ctx, "SHOW CREATE DATABASE `"+strings.ReplaceAll(database, "`", "``")+"`")
	if err != nil {
		if ctx.Err() != nil {
			return OverviewValue{}, ctx.Err()
		}
		state := "query_error"
		var my *mysql.MySQLError
		var ocean *ob.MySQLError
		var number uint16
		if errors.As(err, &my) {
			number = my.Number
		}
		if errors.As(err, &ocean) {
			number = ocean.Number
		}
		switch number {
		case 1044, 1045, 1142, 1143, 1227:
			state = "restricted"
		case 1049:
			return OverviewValue{}, ErrCatalogNotFound
		}
		return OverviewValue{State: state, Source: source + "; native definition could not be read"}, nil
	}
	defer rows.Close()
	columns, err := rows.Columns()
	if err != nil {
		return OverviewValue{}, err
	}
	if len(columns) != 2 || !strings.EqualFold(columns[0], "Database") || !strings.EqualFold(columns[1], "Create Database") {
		return OverviewValue{State: "query_error", Source: source + "; unexpected native result shape"}, nil
	}
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return OverviewValue{}, err
		}
		return OverviewValue{State: "unknown", Source: source + "; no definition returned"}, nil
	}
	var name, text sql.NullString
	if err := rows.Scan(&name, &text); err != nil {
		return OverviewValue{}, err
	}
	if name.String != database || !text.Valid || text.String == "" || !validOverviewText(text.String) || rows.Next() {
		return OverviewValue{State: "query_error", Source: source + "; invalid native definition"}, nil
	}
	if err := rows.Err(); err != nil {
		return OverviewValue{}, err
	}
	value := overviewValue(text, "unknown", source)
	if len(text.String) > 16384 {
		short := text.String[:16384]
		for !validOverviewText(short) {
			short = short[:len(short)-1]
		}
		value.Value = &short
		value.State = "truncated"
		value.Source += "; UTF-8 byte limit 16384; incomplete definition"
	}
	return value, nil
}

func overviewPermissionDenied(err error) bool {
	var my *mysql.MySQLError
	var ocean *ob.MySQLError
	var number uint16
	if errors.As(err, &my) {
		number = my.Number
	}
	if errors.As(err, &ocean) {
		number = ocean.Number
	}
	switch number {
	case 1044, 1045, 1142, 1143, 1227:
		return true
	}
	return false
}
