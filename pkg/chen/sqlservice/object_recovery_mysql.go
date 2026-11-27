package sqlservice

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

// Missing dictionary rows can also mean a revoked grant. For a missing
// object return archived source only with fresh evidence of rights covering
// the original table or schema. Unsupported or role-only evidence fails closed.
func CheckMySQLRecoveryAccess(ctx context.Context, conn *sql.Conn, ref ObjectRef, record *ObjectRecovery) error {
	at := strings.LastIndexByte(record.User, '@')
	if at < 0 || strings.ContainsAny(record.User, "'\\") {
		return ErrCatalogForbidden
	}
	grantee := "'" + record.User[:at] + "'@'" + record.User[at+1:] + "'"
	// Global privileges in MySQL 8 can contain schema-level restrictions.
	// Older servers and MariaDB return no row for this variable.
	var name, value string
	err := conn.QueryRowContext(ctx, "SHOW GLOBAL VARIABLES WHERE Variable_name='partial_revokes'").Scan(&name, &value)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return ErrCatalogForbidden
	}
	global := value == "" || value == "OFF" || value == "0"
	allowed := func(view, scope string, args ...any) bool {
		query := "SELECT EXISTS(SELECT 1 FROM information_schema." + view + " WHERE BINARY GRANTEE=BINARY ? AND PRIVILEGE_TYPE=?" + scope + ")"
		var yes bool
		return conn.QueryRowContext(ctx, query, args...).Scan(&yes) == nil && yes
	}
	const schema = " AND BINARY TABLE_SCHEMA=BINARY ?"
	globalGrant := func(priv string) bool { return global && allowed("USER_PRIVILEGES", "", grantee, priv) }
	schemaGrant := func(priv string) bool {
		return allowed("SCHEMA_PRIVILEGES", schema, grantee, priv, ref.Schema) || globalGrant(priv)
	}
	switch ref.Kind {
	case TriggerKind:
		if allowed("TABLE_PRIVILEGES", schema+" AND BINARY TABLE_NAME=BINARY ?", grantee, "TRIGGER", ref.Schema, ref.Table) || schemaGrant("TRIGGER") {
			return nil
		}
	case EventKind:
		if schemaGrant("EVENT") {
			return nil
		}
	case FunctionKind, ProcedureKind:
		if globalGrant("SELECT") || globalGrant("SHOW_ROUTINE") || schemaGrant("SHOW CREATE ROUTINE") {
			return nil
		}
		if record.User == record.Definer && schemaGrant("CREATE ROUTINE") && schemaGrant("ALTER ROUTINE") {
			return nil
		}
	}
	return ErrCatalogForbidden
}
