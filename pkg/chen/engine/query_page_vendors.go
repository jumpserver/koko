package engine

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"

	dm "gitee.com/chunanyong/dm"
	"github.com/jumpserver/koko/pkg/chen/dialect"
	"github.com/jumpserver/koko/pkg/chen/sqlservice"
)

func vendorQueryPageContext(ctx context.Context, c *console, d dialect.ID) ([]string, error) {
	var query string
	fieldCount := 6 // Oracle-family and Db2 settings.
	switch d {
	case dialect.SQLServer:
		fieldCount = 7
		query = `SELECT COALESCE(DB_NAME(),''),COALESCE(SCHEMA_NAME(),''),SUSER_SNAME(),USER_NAME(),CONVERT(varchar(20),@@OPTIONS),@@LANGUAGE,CONVERT(varchar(20),@@DATEFIRST)`
	case dialect.Oracle, dialect.Dameng, dialect.OceanBaseOracle:
		query = oraclePageIdentity
	case dialect.DB2:
		query = `SELECT RTRIM(CURRENT SERVER),RTRIM(CURRENT SCHEMA),SESSION_USER,CURRENT USER,CURRENT PATH,CHAR(CURRENT TIMEZONE) FROM SYSIBM.SYSDUMMY1`
	case dialect.ClickHouse:
		fieldCount = 3
		query = `SELECT currentDatabase(),currentUser(),timezone()`
	default:
		return nil, errors.New("Database query paging is unavailable for this dialect")
	}
	values, err := readPageContextRow(ctx, c, query, fieldCount)
	if err != nil {
		return nil, err
	}
	settings := ""
	switch d {
	case dialect.SQLServer:
		settings = "DBCC USEROPTIONS WITH NO_INFOMSGS"
	case dialect.Oracle, dialect.OceanBaseOracle:
		settings = "SELECT PARAMETER,COALESCE(VALUE,'') FROM NLS_SESSION_PARAMETERS ORDER BY PARAMETER"
	case dialect.Dameng:
		settings = "SELECT PARAMETER,CASE WHEN VALUE IS NULL THEN '0' ELSE '1'||VALUE END FROM SYS.V$NLS_PARAMETERS ORDER BY PARAMETER"
	case dialect.ClickHouse:
		// clickhouse-go derives max_execution_time from each request's context
		// deadline. Admission and execution have different deadlines; this query
		// budget is not a change to the console's session semantics.
		settings = "SELECT name,value FROM system.settings WHERE changed AND name != 'max_execution_time' ORDER BY name"
	}
	if settings != "" {
		rows, err := c.conn.QueryContext(ctx, settings)
		if err != nil {
			var denied *dm.DmError
			if d == dialect.Dameng && errors.As(err, &denied) && denied.ErrCode == -5504 {
				return nil, &damengNLSAccessError{err, pageContextJSON(values)}
			}
			return nil, err
		}
		defer rows.Close()
		n := 0
		for rows.Next() {
			var key, value string
			if err := rows.Scan(&key, &value); err != nil {
				return nil, err
			}
			values = append(values, key, value)
			n++
			if n > 1024 {
				return nil, errors.New("Paging settings exceed the context limit")
			}
		}
		if err := rows.Err(); err != nil {
			return nil, err
		}
	}
	return values, nil
}

type damengNLSAccessError struct {
	error
	identity string
}

const oraclePageIdentity = `SELECT SYS_CONTEXT('USERENV','DB_NAME'),SYS_CONTEXT('USERENV','CURRENT_SCHEMA'),SYS_CONTEXT('USERENV','CURRENT_USER'),SYS_CONTEXT('USERENV','SESSION_USER'),SESSIONTIMEZONE,DBTIMEZONE FROM DUAL`

func pageContextJSON(values []string) string {
	data, _ := json.Marshal(values)
	return string(data)
}

func readPageContextRow(ctx context.Context, c *console, query string, fieldCount int) ([]string, error) {
	rows, err := c.conn.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	values := make([]string, fieldCount)
	dest := make([]any, fieldCount)
	for i := range values {
		dest[i] = &values[i]
	}
	if !rows.Next() {
		err = rows.Err()
		if err == nil {
			err = errors.New("Unable to verify the paging session context")
		}
		return nil, err
	}
	if err = rows.Scan(dest...); err != nil {
		return nil, err
	}
	if rows.Next() {
		return nil, errors.New("Unexpected paging context result")
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	return values, nil
}

// Choose version-specific catalog SQL once per source inspection. Bind each
// relation separately so its visibility and row policies are checked live.
func versionedPageTableQuery(ctx context.Context, c *console, d dialect.ID) (string, error) {
	switch d {
	case dialect.SQLServer:
		var version string
		var edition int
		if err := c.conn.QueryRowContext(ctx, `SELECT CONVERT(varchar(128),SERVERPROPERTY('ProductVersion')),CONVERT(int,SERVERPROPERTY('EngineEdition'))`).Scan(&version, &edition); err != nil {
			return "", err
		}
		major, ok := dialect.ParseSQLServerMajor(version)
		if !ok || major < 9 {
			return "", errors.New("Database paging requires verified SQL Server 2005 or later metadata")
		}
		// Resolve the original name first, including dbo fallback. Filtering
		// through sys.tables afterwards cannot skip a shadowing view/synonym.
		predicate := `NOT EXISTS(SELECT 1 FROM sys.columns c WHERE c.object_id=t.object_id AND (c.is_computed=1 OR c.user_type_id<>c.system_type_id))`
		if major >= 13 || edition == 5 || edition == 8 {
			predicate = `(HAS_PERMS_BY_NAME(DB_NAME(),'DATABASE','VIEW DEFINITION')=1 OR HAS_PERMS_BY_NAME(NULL,NULL,'VIEW ANY DEFINITION')=1 OR HAS_PERMS_BY_NAME(DB_NAME(),'DATABASE','ALTER ANY SECURITY POLICY')=1) AND t.is_external=0 AND ` + predicate + ` AND NOT EXISTS(SELECT 1 FROM sys.security_predicates p JOIN sys.security_policies x ON x.object_id=p.object_id WHERE p.target_object_id=t.object_id AND x.is_enabled=1)`
		}
		return "SELECT CASE WHEN " + predicate + " THEN 1 ELSE 0 END FROM sys.tables t WHERE t.object_id=OBJECT_ID(@p1)", nil
	case dialect.DB2:
		var level string
		if err := c.conn.QueryRowContext(ctx, `SELECT SERVICE_LEVEL FROM TABLE(SYSPROC.ENV_GET_INST_INFO()) AS CHEN_INSTANCE`).Scan(&level); err != nil {
			return "", err
		}
		v, ok := dialect.ParseDB2ServiceLevel(level)
		if !ok || v.Major < 9 || v.Major == 9 && v.Minor < 7 {
			return "", errors.New("Database paging requires verified DB2 LUW 9.7 or later metadata")
		}
		predicate := `t.TYPE='T' AND NOT EXISTS(SELECT 1 FROM SYSCAT.COLUMNS c WHERE c.TABSCHEMA=t.TABSCHEMA AND c.TABNAME=t.TABNAME AND (c.TYPESCHEMA<>'SYSIBM' OR (c.GENERATED='A' AND c.IDENTITY<>'Y')))`
		if v.Major >= 10 {
			predicate += ` AND NOT EXISTS(SELECT 1 FROM SYSCAT.CONTROLS x WHERE x.TABSCHEMA=t.TABSCHEMA AND x.TABNAME=t.TABNAME AND x.ENABLE='Y')`
		}
		return "SELECT CASE WHEN " + predicate + " THEN 1 ELSE 0 END FROM SYSCAT.TABLES t WHERE t.TABSCHEMA=COALESCE(NULLIF(?,''),CURRENT SCHEMA) AND t.TABNAME=?", nil
	default:
		return "", errors.New("Unsupported versioned table catalog")
	}
}

func vendorQueryPageSource(ctx context.Context, c *console, d dialect.ID, r sqlservice.QueryPageRelation, safe *bool, tableQuery *string) error {
	switch d {
	case dialect.SQLServer, dialect.DB2:
		if *tableQuery == "" {
			query, err := versionedPageTableQuery(ctx, c, d)
			if err != nil {
				return err
			}
			*tableQuery = query
		}
		if d == dialect.DB2 {
			return c.conn.QueryRowContext(ctx, *tableQuery, r.Schema, r.Name).Scan(safe)
		}
		qualified := quoteDatabaseIdentifier(r.Name, d)
		if r.Schema != "" {
			qualified = quoteDatabaseIdentifier(r.Schema, d) + "." + qualified
		}
		return c.conn.QueryRowContext(ctx, *tableQuery, qualified).Scan(safe)
	case dialect.Oracle, dialect.Dameng, dialect.OceanBaseOracle:
		owner := r.Schema
		if owner == "" {
			if err := c.conn.QueryRowContext(ctx, sqlservice.OracleCurrentSchemaSQL).Scan(&owner); err != nil {
				return err
			}
		}
		var one int
		table := `SELECT 1 FROM ALL_TABLES WHERE OWNER=:1 AND TABLE_NAME=:2`
		if d == dialect.Dameng {
			// VSYSOBJTABLES only exposes visible tables. INFO3's low byte is the
			// storage type: require an ordinary user table (external tables use 13).
			table = `SELECT 1 FROM SYS.VSYSOBJTABLES WHERE SF_GET_SCHEMA_NAME_BY_ID(SCHID)=:1 AND NAME=:2 AND SUBTYPE$='UTAB' AND BITAND(INFO3,255)=0`
		}
		err := c.conn.QueryRowContext(ctx, table, owner, r.Name).Scan(&one)
		if errors.Is(err, sql.ErrNoRows) && d == dialect.Oracle && r.Schema == "" && r.Name == "DUAL" {
			var matches, local int
			if err = c.conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM ALL_OBJECTS WHERE OWNER=:1 AND OBJECT_NAME='DUAL'`, owner).Scan(&local); err != nil {
				return err
			}
			if local != 0 {
				*safe = false
				return nil
			}
			err = c.conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM ALL_SYNONYMS WHERE OWNER='PUBLIC' AND SYNONYM_NAME='DUAL' AND TABLE_OWNER='SYS' AND TABLE_NAME='DUAL' AND DB_LINK IS NULL`).Scan(&matches)
			*safe = matches == 1
			return err
		}
		if err != nil {
			return err
		}
		// Independent exact-key reads avoid deeply correlated Oracle catalog
		// views and NULL-typed schema binds while preserving every guard.
		var unsafe int
		checks := []string{
			`SELECT COUNT(*) FROM ALL_TAB_COLS WHERE OWNER=:1 AND TABLE_NAME=:2 AND (VIRTUAL_COLUMN IS NULL OR VIRTUAL_COLUMN<>'NO' OR DATA_TYPE_OWNER IS NOT NULL)`,
			`SELECT COUNT(*) FROM ALL_POLICIES WHERE OBJECT_OWNER=:1 AND OBJECT_NAME=:2 AND (ENABLE IS NULL OR ENABLE<>'NO')`,
		}
		if d != dialect.Dameng {
			checks = append(checks, `SELECT COUNT(*) FROM ALL_EXTERNAL_TABLES WHERE OWNER=:1 AND TABLE_NAME=:2`)
		}
		for _, query := range checks {
			if err := c.conn.QueryRowContext(ctx, query, owner, r.Name).Scan(&unsafe); err != nil {
				return err
			}
			if unsafe != 0 {
				*safe = false
				return nil
			}
		}
		*safe = true
		return nil

	case dialect.ClickHouse:
		var engine string
		var policies int
		err := c.conn.QueryRowContext(ctx, `SELECT engine,
(SELECT count() FROM system.row_policies WHERE database=if(?='',currentDatabase(),?) AND table=?)
FROM system.tables WHERE database=if(?='',currentDatabase(),?) AND name=?`, r.Schema, r.Schema, r.Name, r.Schema, r.Schema, r.Name).Scan(&engine, &policies)
		// External/executable storage can hide behavior absent from the SQL AST.
		*safe = policies == 0 && (engine == "Memory" || strings.HasSuffix(engine, "MergeTree"))
		if err != nil || !*safe {
			return err
		}
		// Missing old-part columns can evaluate DEFAULT/MATERIALIZED on read,
		// not just ALIAS. Inspect every hidden expression before replay.
		rows, err := c.conn.QueryContext(ctx, `SELECT default_expression FROM system.columns WHERE database=if(?='',currentDatabase(),?) AND table=? AND default_expression<>''`, r.Schema, r.Schema, r.Name)
		if err != nil {
			return err
		}
		functions := []string{}
		n := 0
		for rows.Next() {
			n++
			var expr string
			if err = rows.Scan(&expr); err != nil {
				break
			}
			if n > 2048 || len(expr) > 16384 {
				err = errors.New("Hidden column expressions exceed the source inspection limit")
				break
			}
			parsed, e := sqlservice.AnalyzeQueryPage(dialect.ClickHouse, "SELECT "+expr, sqlservice.AnalyzeOptions{})
			if e != nil || len(parsed.Relations) != 0 {
				err = errors.New("Database paging cannot verify a hidden ClickHouse column expression")
				break
			}
			functions = append(functions, parsed.Functions...)
		}
		if err == nil {
			err = rows.Err()
		}
		rows.Close()
		if err != nil {
			return err
		}
		return validateVendorQueryFunctions(ctx, c, d, functions)

	default:
		return errors.New("Unverified query paging source dialect")
	}
}

// Unqualified vendor built-ins can be shadowed through schema/path resolution.
// A conservative catalog check denies any ambiguous visible overload/synonym.
func validateVendorQueryFunctions(ctx context.Context, c *console, d dialect.ID, functions []string) error {
	seen := map[string]bool{}
	for _, name := range functions {
		// Names already use dialect canonical spelling. Quoted function calls are rejected by the grammar; never fold quoted bare references.
		if seen[name] {
			continue
		}
		seen[name] = true
		var shadows int
		var err error
		switch d {
		case dialect.SQLServer:
			continue // T-SQL scalar UDFs require a schema qualifier; the closed grammar rejects it.
		case dialect.Oracle, dialect.Dameng, dialect.OceanBaseOracle:
			err = c.conn.QueryRowContext(ctx, `SELECT
(SELECT COUNT(*) FROM ALL_PROCEDURES WHERE OWNER=SYS_CONTEXT('USERENV','CURRENT_SCHEMA') AND OBJECT_NAME=:1) +
(SELECT COUNT(*) FROM ALL_SYNONYMS WHERE OWNER IN (SYS_CONTEXT('USERENV','CURRENT_SCHEMA'),'PUBLIC') AND SYNONYM_NAME=:2)
FROM DUAL`, name, name).Scan(&shadows)
		case dialect.DB2:
			err = c.conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM SYSCAT.ROUTINES WHERE ROUTINENAME=? AND ROUTINESCHEMA NOT IN('SYSIBM','SYSFUN','SYSPROC','SYSIBMADM')`, name).Scan(&shadows)
		case dialect.ClickHouse:
			err = c.conn.QueryRowContext(ctx, `SELECT count() FROM system.functions WHERE upper(name)=? AND origin<>'System'`, strings.ToUpper(name)).Scan(&shadows)
		default:
			return errors.New("Unverified query function resolution")
		}
		if err != nil || shadows != 0 {
			return errors.New("Database paging cannot verify that each selected function resolves only to a native built-in")
		}
	}
	return nil
}
