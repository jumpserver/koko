package sqlservice

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"
)

var ErrDiagramContractChanged = errors.New("diagram catalog contract changed; refresh the diagram")

// Catalog field probes below determine compatibility. Version numbers alone
// must not disable the relationship feature retained from v5.1.1.
var diagramMariaVersion = regexp.MustCompile(`(?i)MariaDB`)
var diagramOBVersion = regexp.MustCompile(`(?i)OceanBase`)

// Probe on the same authorized connection for each page, independently of
// overview. A fingerprint is meaningful only within the diagram cursor scope.
func probeDiagramMyContract(ctx context.Context, conn *sql.Conn, d Dialect, database string) (contract, source, reason string, columnPrivileges bool, err error) {
	var version, mode, casing string
	if err = conn.QueryRowContext(ctx, "SELECT VERSION(), @@lower_case_table_names").Scan(&version, &casing); err != nil {
		return
	}
	if len(version) > 256 || !validOverviewText(version) {
		err = errors.New("invalid diagram server version")
		return
	}
	source = string(d) + " " + version + "; information_schema"
	if d == MariaDB && !diagramMariaVersion.MatchString(version) || d == OceanBaseMySQL && !diagramOBVersion.MatchString(version) {
		reason = "Unverified product/version for catalog relationships"
		return
	}
	if d == OceanBaseMySQL {
		if err = conn.QueryRowContext(ctx, "SELECT @@ob_compatibility_mode").Scan(&mode); err != nil {
			return
		}
		if !strings.EqualFold(mode, "MYSQL") {
			reason = "Catalog relationships require an OceanBase MySQL tenant"
			return
		}
	}
	required := map[string][]string{
		"TABLES":            {"TABLE_SCHEMA", "TABLE_NAME", "TABLE_TYPE"},
		"COLUMNS":           {"TABLE_SCHEMA", "TABLE_NAME", "COLUMN_NAME"},
		"TABLE_CONSTRAINTS": {"TABLE_SCHEMA", "TABLE_NAME", "CONSTRAINT_NAME", "CONSTRAINT_TYPE"},
		"KEY_COLUMN_USAGE":  {"TABLE_SCHEMA", "TABLE_NAME", "CONSTRAINT_NAME", "COLUMN_NAME", "ORDINAL_POSITION", "REFERENCED_TABLE_SCHEMA", "REFERENCED_TABLE_NAME", "REFERENCED_COLUMN_NAME"},
	}
	rows, e := conn.QueryContext(ctx, `SELECT TABLE_NAME,COLUMN_NAME FROM information_schema.COLUMNS WHERE TABLE_SCHEMA='information_schema' AND TABLE_NAME IN ('TABLES','COLUMNS','TABLE_CONSTRAINTS','KEY_COLUMN_USAGE','COLUMN_PRIVILEGES') ORDER BY TABLE_NAME,ORDINAL_POSITION LIMIT 513`)
	if e != nil {
		err = e
		return
	}
	defer rows.Close()
	names := []string{}
	seen := map[string]bool{}
	for rows.Next() {
		var table, col string
		if err = rows.Scan(&table, &col); err != nil {
			return
		}
		if len(names) >= 512 || !utf8.ValidString(table) || !utf8.ValidString(col) || utf8.RuneCountInString(table) > 128 || utf8.RuneCountInString(col) > 128 {
			err = errors.New("diagram capability budget exceeded")
			return
		}
		key := strings.ToUpper(table) + "." + strings.ToUpper(col)
		seen[key] = true
		names = append(names, key)
	}
	if err = rows.Err(); err != nil {
		return
	}
	for table, cols := range required {
		for _, col := range cols {
			if !seen[table+"."+col] {
				reason = "Required relationship catalog fields are unavailable"
				return
			}
		}
	}
	columnPrivileges = seen["COLUMN_PRIVILEGES.TABLE_SCHEMA"] && seen["COLUMN_PRIVILEGES.TABLE_NAME"]
	sort.Strings(names)
	contract = fmt.Sprintf("%x", sha256.Sum256([]byte("diagram-v1\x00"+string(d)+"\x00"+version+"\x00"+mode+"\x00"+casing+"\x00"+strings.Join(names, ","))))
	return
}

func diagramCompatibleSQL(d Dialect, r DiagramRequest) (string, []any) {
	types := "('BASE TABLE')"
	if d == MariaDB {
		types = "('BASE TABLE','SYSTEM VERSIONED')"
	}
	if r.Section == "nodes" {
		q := `SELECT TABLE_SCHEMA,TABLE_NAME,CASE TABLE_TYPE WHEN 'SYSTEM VERSIONED' THEN 'system_versioned' ELSE 'table' END FROM information_schema.TABLES WHERE BINARY TABLE_SCHEMA=BINARY ? AND TABLE_TYPE IN ` + types
		if r.Direction == "incoming" {
			return q + ` AND BINARY TABLE_NAME=BINARY ?`, []any{r.Database, r.Table}
		}
		return q + ` AND (BINARY TABLE_SCHEMA,BINARY TABLE_NAME)>(BINARY ?,BINARY ?) ORDER BY BINARY TABLE_SCHEMA,BINARY TABLE_NAME LIMIT ?`, []any{r.Database, r.AfterSchema, r.AfterName, r.Limit + 1}
	}
	// Page visible constraint identities before expanding columns. MariaDB hides
	// TABLE_CONSTRAINTS from SELECT-only accounts, so KCU is an independent
	// source. A visible constraint without accessible pairs still remains partial.
	filter := ""
	args := []any{r.Database, r.Database, r.Database}
	if r.Direction == "incoming" {
		filter = ` AND EXISTS (SELECT 1 FROM information_schema.KEY_COLUMN_USAGE i WHERE BINARY i.TABLE_SCHEMA=BINARY c.TABLE_SCHEMA AND BINARY i.TABLE_NAME=BINARY c.TABLE_NAME AND BINARY i.CONSTRAINT_NAME=BINARY c.CONSTRAINT_NAME AND BINARY i.REFERENCED_TABLE_SCHEMA=BINARY ? AND BINARY i.REFERENCED_TABLE_NAME=BINARY ?)`
		args = append(args, r.Schema, r.Table)
	}
	args = append(args, r.AfterSchema, r.AfterTable, r.AfterName, r.Limit+1, r.Database, r.Database, r.Database, r.Database)
	return `WITH c AS (
 SELECT BINARY TABLE_SCHEMA AS TABLE_SCHEMA,BINARY TABLE_NAME AS TABLE_NAME,BINARY CONSTRAINT_NAME AS CONSTRAINT_NAME FROM information_schema.KEY_COLUMN_USAGE WHERE BINARY TABLE_SCHEMA=BINARY ? AND REFERENCED_TABLE_NAME IS NOT NULL
 UNION
 SELECT BINARY TABLE_SCHEMA AS TABLE_SCHEMA,BINARY TABLE_NAME AS TABLE_NAME,BINARY CONSTRAINT_NAME AS CONSTRAINT_NAME FROM information_schema.TABLE_CONSTRAINTS WHERE BINARY TABLE_SCHEMA=BINARY ? AND CONSTRAINT_TYPE='FOREIGN KEY'
 ), p AS (SELECT c.TABLE_SCHEMA,c.TABLE_NAME,c.CONSTRAINT_NAME,COUNT(k.ORDINAL_POSITION) AS pair_count
 FROM c
 JOIN information_schema.TABLES s ON BINARY s.TABLE_SCHEMA=BINARY c.TABLE_SCHEMA AND BINARY s.TABLE_NAME=BINARY c.TABLE_NAME AND s.TABLE_TYPE IN ` + types + `
 LEFT JOIN information_schema.KEY_COLUMN_USAGE k ON BINARY k.TABLE_SCHEMA=BINARY c.TABLE_SCHEMA AND BINARY k.TABLE_NAME=BINARY c.TABLE_NAME AND BINARY k.CONSTRAINT_NAME=BINARY c.CONSTRAINT_NAME
 WHERE BINARY c.TABLE_SCHEMA=BINARY ?` + filter + `
 AND (BINARY c.TABLE_SCHEMA,BINARY c.TABLE_NAME,BINARY c.CONSTRAINT_NAME)>(BINARY ?,BINARY ?,BINARY ?)
 GROUP BY c.TABLE_SCHEMA,c.TABLE_NAME,c.CONSTRAINT_NAME
 ORDER BY BINARY c.TABLE_SCHEMA,BINARY c.TABLE_NAME,BINARY c.CONSTRAINT_NAME LIMIT ?)
 SELECT p.TABLE_SCHEMA,p.TABLE_NAME,p.CONSTRAINT_NAME,
 CASE WHEN BINARY k.REFERENCED_TABLE_SCHEMA<>BINARY ? OR t.TABLE_NAME IS NOT NULL THEN k.REFERENCED_TABLE_SCHEMA END,
 CASE WHEN BINARY k.REFERENCED_TABLE_SCHEMA<>BINARY ? OR t.TABLE_NAME IS NOT NULL THEN k.REFERENCED_TABLE_NAME END,
 t.TABLE_NAME IS NOT NULL,k.ORDINAL_POSITION,p.pair_count,sc.COLUMN_NAME,CASE WHEN BINARY k.REFERENCED_TABLE_SCHEMA<>BINARY ? THEN k.REFERENCED_COLUMN_NAME ELSE tc.COLUMN_NAME END
 FROM p LEFT JOIN information_schema.KEY_COLUMN_USAGE k ON BINARY k.TABLE_SCHEMA=BINARY p.TABLE_SCHEMA AND BINARY k.TABLE_NAME=BINARY p.TABLE_NAME AND BINARY k.CONSTRAINT_NAME=BINARY p.CONSTRAINT_NAME AND k.ORDINAL_POSITION<=64
 LEFT JOIN information_schema.TABLES t ON BINARY t.TABLE_SCHEMA=BINARY ? AND BINARY t.TABLE_SCHEMA=BINARY k.REFERENCED_TABLE_SCHEMA AND BINARY t.TABLE_NAME=BINARY k.REFERENCED_TABLE_NAME AND t.TABLE_TYPE IN ` + types + `
 LEFT JOIN information_schema.COLUMNS sc ON BINARY sc.TABLE_SCHEMA=BINARY k.TABLE_SCHEMA AND BINARY sc.TABLE_NAME=BINARY k.TABLE_NAME AND BINARY sc.COLUMN_NAME=BINARY k.COLUMN_NAME
 LEFT JOIN information_schema.COLUMNS tc ON BINARY tc.TABLE_SCHEMA=BINARY t.TABLE_SCHEMA AND BINARY tc.TABLE_NAME=BINARY t.TABLE_NAME AND BINARY tc.COLUMN_NAME=BINARY k.REFERENCED_COLUMN_NAME
 ORDER BY BINARY p.TABLE_SCHEMA,BINARY p.TABLE_NAME,BINARY p.CONSTRAINT_NAME,k.ORDINAL_POSITION`, args
}
