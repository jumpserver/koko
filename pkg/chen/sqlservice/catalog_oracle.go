package sqlservice

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/jumpserver/koko/pkg/chen/dialect"
	gooraerr "github.com/sijms/go-ora/v2/network"
)

// OracleCurrentSchemaSQL reads the effective schema of the current physical session.
const OracleCurrentSchemaSQL = "SELECT SYS_CONTEXT('USERENV','CURRENT_SCHEMA') FROM DUAL"

func oracleApplicationOwner(d dialect.ID, owner string) string {
	if d == dialect.Oracle {
		// Oracle 12c+ explicitly marks maintained accounts. Avoid fragile APEX
		// version-name lists, and do not assume this column exists in DM/OB.
		return " AND " + owner + " IN (SELECT USERNAME FROM ALL_USERS WHERE ORACLE_MAINTAINED = 'N')"
	}
	return ""
}

// Oracle before 12c has no ORACLE_MAINTAINED flag. A retry removes only that
// optional edit classification filter. ALL_* authorization, the edit path's
// explicit system-owner guard, caller scope and bind arguments stay unchanged.
func queryCatalogRows(ctx context.Context, conn *sql.Conn, d dialect.ID, query string, args ...any) (*sql.Rows, error) {
	rows, err := conn.QueryContext(ctx, query, args...)
	if err == nil || d != dialect.Oracle || !strings.Contains(query, "ORACLE_MAINTAINED") {
		return rows, err
	}
	var oracle *gooraerr.OracleError
	if !errors.As(err, &oracle) || oracle.ErrCode != 904 {
		return nil, err
	}
	fallback := query
	for _, owner := range []string{"o.OWNER", "f.OWNER", "schemaname"} {
		fallback = strings.ReplaceAll(fallback, oracleApplicationOwner(dialect.Oracle, owner), "")
	}
	if fallback == query {
		return nil, err
	}
	return conn.QueryContext(ctx, fallback, args...)
}

// DBA_* is optional: SELECT on a business table does not grant segment metadata.
// A failed optional privilege probe never triggers a privilege change.
func overviewOracleSegmentAccess(ctx context.Context, conn *sql.Conn) (bool, error) {
	rows, err := conn.QueryContext(ctx, "SELECT BYTES FROM DBA_SEGMENTS WHERE 1=0")
	if err != nil {
		var oracle *gooraerr.OracleError
		if errors.As(err, &oracle) && (oracle.ErrCode == 942 || oracle.ErrCode == 1031) {
			return false, nil
		}
		return false, err
	}
	return true, rows.Close()
}

func overviewOracleSegmentJoin(broad bool) string {
	segments, indexes, lobs, owner := "USER_SEGMENTS", "USER_INDEXES", "USER_LOBS", "USER"
	if broad {
		segments, indexes, lobs, owner = "DBA_SEGMENTS", "ALL_INDEXES", "ALL_LOBS", "s.OWNER"
	}
	tableRows := `SELECT ` + owner + ` AS owner,s.SEGMENT_NAME AS table_name,s.BYTES,'data' AS category FROM ` + segments + ` s WHERE s.SEGMENT_TYPE IN ('TABLE','TABLE PARTITION','TABLE SUBPARTITION')`
	indexOwner, indexJoin := "i.TABLE_OWNER", ""
	lobOwner, lobJoin := "USER", ""
	if broad {
		indexJoin = " AND i.OWNER=s.OWNER"
		lobOwner = "l.OWNER"
		lobJoin = " AND l.OWNER=s.OWNER"
	}
	return ` LEFT JOIN (SELECT owner,table_name,SUM(CASE WHEN category='data' THEN bytes ELSE 0 END) AS data_bytes,SUM(CASE WHEN category='index' THEN bytes ELSE 0 END) AS index_bytes,SUM(bytes) AS total_bytes FROM (` + tableRows + `
 UNION ALL SELECT ` + indexOwner + `,i.TABLE_NAME,s.BYTES,'index' FROM ` + segments + ` s JOIN ` + indexes + ` i ON i.INDEX_NAME=s.SEGMENT_NAME` + indexJoin + ` WHERE s.SEGMENT_TYPE IN ('INDEX','INDEX PARTITION','INDEX SUBPARTITION')
 UNION ALL SELECT ` + lobOwner + `,l.TABLE_NAME,s.BYTES,CASE WHEN s.SEGMENT_NAME=l.INDEX_NAME THEN 'index' ELSE 'data' END FROM ` + segments + ` s JOIN ` + lobs + ` l ON s.SEGMENT_NAME IN (l.SEGMENT_NAME,l.INDEX_NAME)` + lobJoin + `
 ) segments GROUP BY owner,table_name) z ON z.owner=o.schemaname AND z.table_name=o.objectname`
}
