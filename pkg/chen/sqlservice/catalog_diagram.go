package sqlservice

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jumpserver/koko/pkg/chen/dialect"
)

const DiagramMaxPairs = 64
const DiagramPageBytes = 512 * 1024

type DiagramRequest struct {
	CatalogObjectRequest
	Section, Direction, Table, AfterTable string
	Contract                              string
	Continuation                          bool
}
type DiagramPair struct {
	Ordinal int     `json:"ordinal"`
	Source  *string `json:"source"`
	Target  *string `json:"target"`
}
type DiagramEdge struct {
	ColumnCount    int            `json:"column_count"`
	Name           string         `json:"name"`
	Source         CatalogObject  `json:"source"`
	Target         *CatalogObject `json:"target"`
	TargetOpenable bool           `json:"target_openable"`
	State          string         `json:"state"`
	Pairs          []DiagramPair  `json:"pairs"`
}
type DiagramItem struct {
	Object *CatalogObject `json:"object,omitempty"`
	Edge   *DiagramEdge   `json:"edge,omitempty"`
}
type CatalogDiagram struct {
	Contract   string        `json:"-"`
	Reason     string        `json:"reason,omitempty"`
	Database   string        `json:"database"`
	Schema     string        `json:"schema"`
	Table      string        `json:"table"`
	Direction  string        `json:"direction"`
	Section    string        `json:"section"`
	State      string        `json:"state"`
	Source     string        `json:"source"`
	Visibility string        `json:"visibility"`
	Snapshot   bool          `json:"snapshot"`
	SampledAt  string        `json:"sampled_at"`
	Items      []DiagramItem `json:"items"`
	HasMore    bool          `json:"has_more"`
	NextCursor string        `json:"next_cursor,omitempty"`
}

// Two independent keyset streams: visible tables and complete constraints. A
// page never splits a composite FK. Only catalog connections/queries are used.
func CatalogDiagramRows(ctx context.Context, conn *sql.Conn, d dialect.ID, r DiagramRequest) (result CatalogDiagram, err error) {
	defer func() {
		if overviewPermissionDenied(err) {
			err = ErrCatalogForbidden
		}
	}()
	result = CatalogDiagram{Database: r.Database, Schema: r.Schema, Table: r.Table, Direction: r.Direction, Section: r.Section, State: "complete", Visibility: "authorized_catalog", SampledAt: time.Now().UTC().Format(time.RFC3339Nano), Items: []DiagramItem{}}
	if err := validateCatalogInput(conn, d, r.Database, r.Schema, r.Table, r.AfterSchema, r.AfterName, r.AfterTable); err != nil {
		return result, err
	}
	if (r.Section != "nodes" && r.Section != "edges") || (r.Direction != "schema" && r.Direction != "incoming") || r.Direction == "incoming" && (r.Table == "" || r.Schema == "") {
		return result, fmt.Errorf("invalid diagram request")
	}
	if d != dialect.PostgreSQL && !d.UsesMySQLSyntax() && !extraCatalogDialect(d) {
		result.State = "unsupported"
		return result, nil
	}
	result.Source = "information_schema"
	if d == dialect.PostgreSQL {
		result.Source = "pg_catalog"
	}
	if (d.UsesMySQLSyntax() || d == dialect.ClickHouse) && r.Schema != "" && r.Schema != r.Database {
		return result, ErrCatalogForbidden
	}
	if err := catalogDatabaseAccess(ctx, conn, d, r.Database); err != nil {
		return result, err
	}
	if d == dialect.PostgreSQL && r.Schema != "" {
		if err := catalogSchemaAccess(ctx, conn, r.Schema); err != nil {
			return result, err
		}
	}
	columnPrivileges := false
	if d == dialect.MariaDB || d == dialect.OceanBaseMySQL {
		result.Contract, result.Source, result.Reason, columnPrivileges, err = probeDiagramMyContract(ctx, conn, d, r.Database)
		if err != nil {
			return result, err
		}
		if r.Continuation && (result.Contract == "" || r.Contract != result.Contract) {
			return result, ErrDiagramContractChanged
		}
		if result.Reason != "" {
			result.State = "unsupported"
			return result, nil
		}
	}
	// Column-only grants can hide an entire FK in KCU, not just its column
	// names. Be conservative even if the account also holds wider grants.
	if d == dialect.MariaDB || d == dialect.OceanBaseMySQL {
		var columnGrants bool
		if !columnPrivileges {
			result.State = "partial"
			result.Reason = "Column privilege visibility cannot be verified; relationships are limited to the visible catalog"
		} else {
			err = conn.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM information_schema.COLUMN_PRIVILEGES p JOIN information_schema.TABLES t ON BINARY t.TABLE_SCHEMA=BINARY p.TABLE_SCHEMA AND BINARY t.TABLE_NAME=BINARY p.TABLE_NAME WHERE BINARY p.TABLE_SCHEMA=BINARY ?)`, r.Database).Scan(&columnGrants)
			if err != nil {
				return result, err
			}
		}
		if columnGrants {
			result.State = "partial"
			result.Reason = "Column privileges may hide entire constraints; relationships are limited to the visible catalog"
		}
	}
	if r.Direction == "incoming" {
		if err := diagramObjectAccess(ctx, conn, d, r); err != nil {
			return result, err
		}
	}
	r.Limit = min(catalogLimit(r.Limit), 100)
	if extraCatalogDialect(d) {
		result.Contract = string(d) + ":authorized_catalog:v1"
		result.Source = string(d) + "; authorized system catalog"
		if r.Continuation && r.Contract != result.Contract {
			return result, ErrDiagramContractChanged
		}
		if d == dialect.ClickHouse && r.Section == "edges" {
			result.State = "unsupported"
			result.Reason = "ClickHouse has no foreign-key relationship catalog"
			return result, nil
		}
	}
	query, args := diagramSQL(d, r)
	if extraCatalogDialect(d) {
		q, e := diagramExtraSQL(d, r)
		if e != nil {
			return result, e
		}
		query, args = q.SQL, q.Args
	}
	if d == dialect.MariaDB || d == dialect.OceanBaseMySQL {
		query, args = diagramCompatibleSQL(d, r)
	}
	rows, err := queryCatalogRows(ctx, conn, d, query, args...)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	for rows.Next() {
		if r.Section == "nodes" {
			o := CatalogObject{Database: r.Database, Kind: "table"}
			if err := rows.Scan(&o.Schema, &o.Name, &o.ObjectType); err != nil {
				return result, err
			}
			result.Items = append(result.Items, DiagramItem{Object: &o})
			continue
		}
		var schema, table, name string
		var targetSchema, targetName, sourceColumn, targetColumn sql.NullString
		var ordinal sql.NullInt64
		var count int
		var openable bool
		if err := rows.Scan(&schema, &table, &name, &targetSchema, &targetName, &openable, &ordinal, &count, &sourceColumn, &targetColumn); err != nil {
			return result, err
		}
		var edge *DiagramEdge
		if n := len(result.Items); n > 0 {
			last := result.Items[n-1].Edge
			if last.Source.Schema == schema && last.Source.Name == table && last.Name == name {
				edge = last
			}
		}
		if edge == nil {
			edge = &DiagramEdge{ColumnCount: count, Name: name, Source: CatalogObject{Database: r.Database, Schema: schema, Name: table, Kind: "table", ObjectType: "table"}, State: "complete", Pairs: []DiagramPair{}, TargetOpenable: openable}
			if targetSchema.Valid && targetName.Valid {
				database := r.Database
				if d.UsesMySQLSyntax() {
					database = targetSchema.String
				}
				edge.Target = &CatalogObject{Database: database, Schema: targetSchema.String, Name: targetName.String, Kind: "table", ObjectType: "table"}
			} else {
				edge.State = "restricted"
			}
			result.Items = append(result.Items, DiagramItem{Edge: edge})
		}
		if !ordinal.Valid || ordinal.Int64 < 1 || ordinal.Int64 > DiagramMaxPairs || len(edge.Pairs) >= DiagramMaxPairs {
			edge.State = "partial"
			continue
		}
		// Reject inconsistent targets/counts and duplicate pairs rather than emitting
		// a complete relationship assembled from changing or malformed metadata.
		if count != edge.ColumnCount || edge.TargetOpenable != openable || (edge.Target != nil && (!targetSchema.Valid || !targetName.Valid || edge.Target.Schema != targetSchema.String || edge.Target.Name != targetName.String)) {
			edge.State = "partial"
			edge.TargetOpenable = false
		}
		if len(edge.Pairs) > 0 && int(ordinal.Int64) <= edge.Pairs[len(edge.Pairs)-1].Ordinal {
			edge.State = "partial"
			continue
		}
		pair := DiagramPair{Ordinal: int(ordinal.Int64)}
		if sourceColumn.Valid {
			v := sourceColumn.String
			pair.Source = &v
		}
		if targetColumn.Valid {
			v := targetColumn.String
			pair.Target = &v
		}
		if (pair.Source == nil || pair.Target == nil) && edge.State != "partial" {
			edge.State = "restricted"
		}
		if pair.Ordinal != len(edge.Pairs)+1 || count > DiagramMaxPairs {
			edge.State = "partial"
		}
		edge.Pairs = append(edge.Pairs, pair)
	}
	if err := rows.Err(); err != nil {
		return result, err
	}
	if len(result.Items) > r.Limit {
		result.HasMore = true
		result.Items = result.Items[:r.Limit]
	}
	size := 0
	for i, item := range result.Items {
		b, _ := json.Marshal(item)
		if size+len(b) > DiagramPageBytes {
			result.Items = result.Items[:i]
			result.HasMore = true
			break
		}
		size += len(b)
		if item.Edge != nil && (len(item.Edge.Pairs) != item.Edge.ColumnCount || item.Edge.ColumnCount < 1) {
			item.Edge.State = "partial"
		}
		if item.Edge != nil && item.Edge.State != "complete" {
			result.State = "partial"
		}
	}
	if result.HasMore {
		result.State = "partial"
	}
	return result, nil
}

func diagramSQL(d dialect.ID, r DiagramRequest) (string, []any) {
	if r.Section == "nodes" {
		if r.Direction == "incoming" {
			if d == dialect.PostgreSQL {
				return `SELECT n.nspname,c.relname,CASE c.relkind WHEN 'p' THEN 'partitioned_table' ELSE 'table' END FROM pg_catalog.pg_class c JOIN pg_catalog.pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname=$1 AND c.relname=$2 AND c.relkind IN ('r','p','f')`, []any{r.Schema, r.Table}
			}
			return `SELECT TABLE_SCHEMA,TABLE_NAME,'table' FROM information_schema.TABLES WHERE BINARY TABLE_SCHEMA=BINARY ? AND BINARY TABLE_NAME=BINARY ? AND TABLE_TYPE='BASE TABLE'`, []any{r.Schema, r.Table}
		}
		if d == dialect.PostgreSQL {
			return `SELECT n.nspname,c.relname,CASE c.relkind WHEN 'p' THEN 'partitioned_table' WHEN 'f' THEN 'foreign_table' ELSE 'table' END` + overviewPGFrom + ` AND c.relkind IN ('r','p','f') AND (n.nspname,c.relname) > ($3::pg_catalog.name,$4::pg_catalog.name) ORDER BY n.nspname,c.relname LIMIT $5`, []any{r.Database, r.Schema, r.AfterSchema, r.AfterName, r.Limit + 1}
		}
		return `SELECT TABLE_SCHEMA,TABLE_NAME,'table' FROM information_schema.TABLES WHERE BINARY TABLE_SCHEMA=BINARY ? AND TABLE_TYPE='BASE TABLE' AND (BINARY TABLE_SCHEMA,BINARY TABLE_NAME)>(BINARY ?,BINARY ?) ORDER BY BINARY TABLE_SCHEMA,BINARY TABLE_NAME LIMIT ?`, []any{r.Database, r.AfterSchema, r.AfterName, r.Limit + 1}
	}
	if d == dialect.PostgreSQL {
		// Keep source and target schemas within the native visible-schema list.
		// Constraint/column metadata does not require SELECT on business tables.
		scope := `($2='' OR n.nspname=$2)`
		if r.Direction == "incoming" {
			scope = `nr.nspname=$2 AND tr.relname=$3 AND nr.nspname IN (SELECT schema_name FROM information_schema.schemata)`
		}
		return `WITH p AS (SELECT k.*,n.nspname AS source_schema,c.relname AS source_table,nr.nspname AS target_schema,tr.relname AS target_table,
   (nr.nspname IN (SELECT schema_name FROM information_schema.schemata)) AS target_visible
   FROM pg_catalog.pg_constraint k JOIN pg_catalog.pg_class c ON c.oid=k.conrelid JOIN pg_catalog.pg_namespace n ON n.oid=c.relnamespace JOIN pg_catalog.pg_class tr ON tr.oid=k.confrelid JOIN pg_catalog.pg_namespace nr ON nr.oid=tr.relnamespace
   WHERE pg_catalog.current_database()=$1 AND k.contype='f' AND ` + scope + ` AND ($3::text IS NOT NULL)
   AND n.nspname IN (SELECT schema_name FROM information_schema.schemata)
   AND (n.nspname,c.relname,k.conname) > ($4::pg_catalog.name,$5::pg_catalog.name,$6::pg_catalog.name)
   ORDER BY n.nspname,c.relname,k.conname LIMIT $7)
  SELECT p.source_schema,p.source_table,p.conname,CASE WHEN p.target_visible THEN p.target_schema END,CASE WHEN p.target_visible THEN p.target_table END,p.target_visible,u.ord,cardinality(p.conkey),
   a.attname, CASE WHEN p.target_visible THEN b.attname END
  FROM p CROSS JOIN LATERAL unnest(p.conkey,p.confkey) WITH ORDINALITY u(a,b,ord)
  LEFT JOIN pg_catalog.pg_attribute a ON a.attrelid=p.conrelid AND a.attnum=u.a LEFT JOIN pg_catalog.pg_attribute b ON b.attrelid=p.confrelid AND b.attnum=u.b
  WHERE u.ord<=64 ORDER BY p.source_schema,p.source_table,p.conname,u.ord`, []any{r.Database, r.Schema, r.Table, r.AfterSchema, r.AfterTable, r.AfterName, r.Limit + 1}
	}
	filter := ""
	args := []any{r.Database}
	if r.Direction == "incoming" {
		filter = ` AND BINARY k.REFERENCED_TABLE_SCHEMA=BINARY ? AND BINARY k.REFERENCED_TABLE_NAME=BINARY ?`
		args = append(args, r.Schema, r.Table)
	}
	args = append(args, r.AfterSchema, r.AfterTable, r.AfterName, r.Limit+1, r.Database, r.Database, r.Database, r.Database)
	return `WITH p AS (SELECT k.TABLE_SCHEMA,k.TABLE_NAME,k.CONSTRAINT_NAME,COUNT(*) AS pair_count
 FROM information_schema.KEY_COLUMN_USAGE k JOIN information_schema.TABLES s ON BINARY s.TABLE_SCHEMA=BINARY k.TABLE_SCHEMA AND BINARY s.TABLE_NAME=BINARY k.TABLE_NAME
 WHERE BINARY k.TABLE_SCHEMA=BINARY ? AND k.REFERENCED_TABLE_NAME IS NOT NULL` + filter + `
 AND (BINARY k.TABLE_SCHEMA,BINARY k.TABLE_NAME,BINARY k.CONSTRAINT_NAME)>(BINARY ?,BINARY ?,BINARY ?)
 GROUP BY k.TABLE_SCHEMA,k.TABLE_NAME,k.CONSTRAINT_NAME ORDER BY BINARY k.TABLE_SCHEMA,BINARY k.TABLE_NAME,BINARY k.CONSTRAINT_NAME LIMIT ?)
 SELECT k.TABLE_SCHEMA,k.TABLE_NAME,k.CONSTRAINT_NAME,
 CASE WHEN BINARY k.REFERENCED_TABLE_SCHEMA<>BINARY ? OR t.TABLE_NAME IS NOT NULL THEN k.REFERENCED_TABLE_SCHEMA END,
 CASE WHEN BINARY k.REFERENCED_TABLE_SCHEMA<>BINARY ? OR t.TABLE_NAME IS NOT NULL THEN k.REFERENCED_TABLE_NAME END,
 t.TABLE_NAME IS NOT NULL,k.ORDINAL_POSITION,p.pair_count,sc.COLUMN_NAME,CASE WHEN BINARY k.REFERENCED_TABLE_SCHEMA<>BINARY ? THEN k.REFERENCED_COLUMN_NAME ELSE tc.COLUMN_NAME END
 FROM p JOIN information_schema.KEY_COLUMN_USAGE k ON BINARY k.TABLE_SCHEMA=BINARY p.TABLE_SCHEMA AND BINARY k.TABLE_NAME=BINARY p.TABLE_NAME AND BINARY k.CONSTRAINT_NAME=BINARY p.CONSTRAINT_NAME
 LEFT JOIN information_schema.TABLES t ON BINARY t.TABLE_SCHEMA=BINARY ? AND BINARY t.TABLE_SCHEMA=BINARY k.REFERENCED_TABLE_SCHEMA AND BINARY t.TABLE_NAME=BINARY k.REFERENCED_TABLE_NAME
 LEFT JOIN information_schema.COLUMNS sc ON BINARY sc.TABLE_SCHEMA=BINARY k.TABLE_SCHEMA AND BINARY sc.TABLE_NAME=BINARY k.TABLE_NAME AND BINARY sc.COLUMN_NAME=BINARY k.COLUMN_NAME
 LEFT JOIN information_schema.COLUMNS tc ON BINARY tc.TABLE_SCHEMA=BINARY t.TABLE_SCHEMA AND BINARY tc.TABLE_NAME=BINARY t.TABLE_NAME AND BINARY tc.COLUMN_NAME=BINARY k.REFERENCED_COLUMN_NAME
 WHERE k.ORDINAL_POSITION<=64 ORDER BY BINARY k.TABLE_SCHEMA,BINARY k.TABLE_NAME,BINARY k.CONSTRAINT_NAME,k.ORDINAL_POSITION`, args
}

func diagramObjectAccess(ctx context.Context, conn *sql.Conn, d dialect.ID, r DiagramRequest) error {
	if d != dialect.MariaDB && d != dialect.OceanBaseMySQL {
		return CatalogObjectAccess(ctx, conn, d, r.Schema, r.Table)
	}
	q, args := diagramCompatibleSQL(d, DiagramRequest{CatalogObjectRequest: r.CatalogObjectRequest, Section: "nodes", Direction: "incoming", Table: r.Table})
	var schema, name, kind string
	err := conn.QueryRowContext(ctx, q, args...).Scan(&schema, &name, &kind)
	if err == sql.ErrNoRows {
		return ErrCatalogNotFound
	}
	return err
}
