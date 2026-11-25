package sqlservice

import (
	"fmt"
	"strings"
)

// Interactive vendor SQL is classified for Query/Exec dispatch, not rewritten.
// This bounded scanner supports ordinary DML/DDL and transaction
// commands, not procedural bodies or client batch directives. Those require a
// vendor grammar that can prove each statement boundary before policy checks.
func analyzeCommandDocument(d Dialect, document string, options AnalyzeOptions) (Analysis, error) {
	return scanCommandDocument(d, document, options, func(ts []token) (string, bool, bool, error) {
		kind, at, err := commandHead(ts)
		if err != nil {
			return "", false, false, err
		}
		// OceanBase Oracle uses this session statement to switch the console schema.
		if d == OceanBaseOracle && len(ts) == 6 && commandIs(ts[0], "ALTER") && commandIs(ts[1], "SESSION") &&
			commandIs(ts[2], "SET") && commandIs(ts[3], "CURRENT_SCHEMA") && ts[4].kind == '=' && ts[5].ident() {
			return "session", false, false, nil
		}
		if err := validateCommandBoundaries(d, document, ts, kind, at); err != nil {
			return "", false, false, err
		}
		rows, write := false, true
		switch kind {
		case "SELECT":
			rows = true
		case "SHOW", "DESCRIBE", "DESC":
			rows, write = true, false
		case "INSERT", "UPDATE", "DELETE", "MERGE", "UPSERT", "REPLACE":
		case "CREATE", "ALTER", "DROP", "TRUNCATE", "RENAME", "COMMENT", "GRANT", "REVOKE", "DENY", "OPTIMIZE", "SYSTEM", "ANALYZE", "VACUUM", "REORG", "RUNSTATS", "REFRESH":
		case "COMMIT", "ROLLBACK", "SAVEPOINT", "RELEASE", "SET", "USE", "SAVE":
			write = false
		case "BEGIN":
			if d != SQLServer || len(ts) < 2 || !commandIs(ts[1], "TRAN", "TRANSACTION", "DISTRIBUTED") {
				return "", false, false, fmt.Errorf("procedural BEGIN blocks require vendor grammar support; use a transaction command supported by this database")
			}
			write = false
		case "START":
			if len(ts) < 2 || !ts[1].is("TRANSACTION") {
				return "", false, false, fmt.Errorf("unsupported START command")
			}
			write = false
		case "CALL", "EXEC", "EXECUTE":
			rows = true
		case "KILL":
			rows = d == ClickHouse
		case "EXPLAIN":
			rows = true // Arbitrary EXPLAIN may execute a write; the estimated-plan endpoint validates separately.
		default:
			return "", false, false, fmt.Errorf("unsupported %s command %s at byte %d", d, kind, ts[0].start)
		}
		// SQL Server OUTPUT is a result set unless routed INTO another table.
		// Oracle RETURNING INTO binds output parameters and is not a result set.
		if d == SQLServer && commandIs(ts[at], "INSERT", "UPDATE", "DELETE", "MERGE") {
			depth, output := 0, false
			for _, t := range ts[at+1:] {
				switch t.kind {
				case '(':
					depth++
				case ')':
					depth--
				}
				if depth != 0 {
					continue
				}
				if t.is("OUTPUT") {
					output, rows = true, true
				}
				if output && t.is("INTO") {
					rows = false
					break
				}
			}
		}
		if d == MariaDB && commandIs(ts[at], "INSERT", "DELETE", "REPLACE") {
			for _, t := range ts[at+1:] {
				if t.is("RETURNING") {
					rows = true
				}
			}
		}
		if kind == "SELECT" {
			// Keep strict read classification for export/AI/estimated-plan gates.
			_, err := analyzeSelectDocument(d, document[ts[0].start:ts[len(ts)-1].end], options)
			write = err != nil
			depth := 0
			for _, t := range ts[at+1:] {
				switch t.kind {
				case '(':
					depth++
				case ')':
					depth--
				}
				if depth == 0 && t.is("INTO") {
					rows = false
				}
			}
		}
		return strings.ToLower(kind), rows, write, nil
	})
}

// ChangesConsoleContext restricts readback to namespace commands. In particular,
// a probe after BEGIN or SET TRANSACTION could establish a premature PG snapshot.
func ChangesConsoleContext(d Dialect, stmt Statement) bool {
	if stmt.Kind == "use" {
		return true
	}
	switch stmt.Kind {
	case "session", "variableset", "set", "transaction", "ddl", "alter", "discard":
	default:
		return false
	}
	var head [3]token
	i := 0
	for _, t := range lex(stmt.SQL, d) {
		if t.kind == 'c' {
			continue
		}
		head[i] = t
		i++
		if i == len(head) {
			break
		}
	}
	switch {
	case commandIs(head[0], "USE"), commandIs(head[0], "ALTER") && commandIs(head[1], "SESSION"):
		return true
	case commandIs(head[0], "SET"):
		return commandIs(head[1], "SCHEMA", "SEARCH_PATH", "ROLE") ||
			commandIs(head[1], "CURRENT", "LOCAL", "SESSION") && commandIs(head[2], "SCHEMA", "SEARCH_PATH", "ROLE", "AUTHORIZATION")
	case d == PostgreSQL:
		return commandIs(head[0], "COMMIT", "END", "ROLLBACK", "ABORT", "DISCARD", "RESET")
	}
	return false
}

func commandIs(t token, choices ...string) bool {
	if t.kind != 'i' || t.quoted {
		return false
	}
	for _, choice := range choices {
		if t.is(choice) {
			return true
		}
	}
	return false
}

// The command set includes unsupported T-SQL statement families because T-SQL
// accepts whitespace as a batch boundary. Unknown procedural/control commands
// must never be hidden in a SELECT classified as safe to rerun for export.
func commandStart(t token) bool {
	return commandIs(t, "SELECT", "INSERT", "UPDATE", "DELETE", "MERGE", "UPSERT", "REPLACE", "CREATE", "ALTER", "DROP", "TRUNCATE", "RENAME", "COMMENT", "GRANT", "REVOKE", "DENY", "BEGIN", "START", "COMMIT", "ROLLBACK", "SAVE", "SAVEPOINT", "RELEASE", "SET", "USE", "SHOW", "DESCRIBE", "DESC", "EXPLAIN", "CALL", "EXEC", "EXECUTE", "FETCH", "ANALYZE", "VACUUM", "REORG", "RUNSTATS", "REFRESH") || unsupportedCommandWord(t)
}

func unsupportedCommandWord(t token) bool {
	return commandIs(t, "KILL", "PRINT", "WAITFOR", "DBCC", "BACKUP", "RESTORE", "DUMP", "LOAD", "BULK", "DECLARE", "DEALLOCATE", "OPEN", "CLOSE", "CHECKPOINT", "RECONFIGURE", "SHUTDOWN", "SETUSER", "IF", "WHILE", "GOTO", "BREAK", "CONTINUE", "RETURN", "RAISERROR", "THROW", "SEND", "RECEIVE", "MOVE", "GET", "ENABLE", "DISABLE", "WRITETEXT", "UPDATETEXT", "READTEXT", "ATTACH", "DETACH", "SYSTEM", "OPTIMIZE", "FLUSH", "RESET", "PURGE", "REPAIR", "ABORT", "DISK", "DO", "HANDLER", "LOCK", "UNLOCK")
}

// SYSTEM is also the ordinary ClickHouse system database name. Followed by a
// dot and an identifier it cannot begin a SYSTEM administration command.
func clickHouseSystemQualifier(d Dialect, ts []token, i int) bool {
	return d == ClickHouse && ts[i].is("SYSTEM") && i+2 < len(ts) && ts[i+1].kind == '.' && ts[i+2].ident()
}

func commandHead(ts []token) (string, int, error) {
	if len(ts) == 0 || ts[0].kind != 'i' || ts[0].quoted {
		return "", 0, fmt.Errorf("expected a SQL command")
	}
	kind, at := strings.ToUpper(ts[0].value), 0
	if kind != "WITH" {
		return kind, at, nil
	}
	depth := 0
	for i := 1; i < len(ts); i++ {
		switch ts[i].kind {
		case '(':
			depth++
		case ')':
			depth--
		}
		if depth == 0 && commandIs(ts[i], "SELECT", "INSERT", "UPDATE", "DELETE", "MERGE") {
			return strings.ToUpper(ts[i].value), i, nil
		}
	}
	return "", 0, fmt.Errorf("WITH requires a complete query or data command")
}

func validateCommandBoundaries(d Dialect, document string, ts []token, kind string, at int) error {
	boundary := func(t token) error {
		return fmt.Errorf("unsupported or ambiguous command boundary near %s at byte %d; use supported ordinary SQL and separate statements with semicolons", t.value, t.start)
	}
	if kind == "GRANT" || kind == "REVOKE" || kind == "DENY" {
		return validatePrivilegeCommand(ts)
	}
	ddlObject := ""
	if kind == "CREATE" || kind == "ALTER" || kind == "DROP" {
		pos := at + 1
		if kind == "CREATE" && pos+1 < len(ts) && ts[pos].is("OR") && ts[pos+1].is("REPLACE") {
			pos += 2
		}
		for pos < len(ts) && commandIs(ts[pos], "UNIQUE", "CLUSTERED", "NONCLUSTERED", "TEMP", "TEMPORARY", "GLOBAL", "LOCAL") {
			pos++
		}
		if pos >= len(ts) || !commandIs(ts[pos], "TABLE", "INDEX", "VIEW", "SEQUENCE", "SCHEMA", "DATABASE", "USER", "ROLE", "SYNONYM", "TABLESPACE", "PROCEDURE", "FUNCTION", "TRIGGER", "TYPE", "MATERIALIZED") {
			return fmt.Errorf("unknown DDL object; use a supported server SQL statement")
		}
		ddlObject = strings.ToUpper(ts[pos].value)
		if ddlObject == "MATERIALIZED" && pos+1 < len(ts) && ts[pos+1].is("VIEW") {
			ddlObject = "VIEW"
		}
	}
	depth, caseDepth := 0, 0
	sawSourceSelect, sawInsertValues, sawSet, alterMutation := false, false, false, false
	mergeAction := ""
	for i, t := range ts {
		switch t.kind {
		case '(':
			depth++
		case ')':
			depth--
		}
		if t.kind != 'i' || t.quoted {
			continue
		}
		if clickHouseSystemQualifier(d, ts, i) {
			continue
		}
		if t.is("DELIMITER") || t.is("GO") && standaloneDirective(document, t) {
			return fmt.Errorf("client batch directives are unsupported; separate SQL commands with semicolons")
		}
		if depth != 0 {
			continue
		}
		if t.is("CASE") {
			caseDepth++
			continue
		}
		if t.is("END") {
			if caseDepth > 0 {
				caseDepth--
				continue
			}
			return boundary(t)
		}
		if i <= at {
			continue
		}
		prev, next := ts[i-1], token{}
		if i+1 < len(ts) {
			next = ts[i+1]
		}
		if t.is("VALUES") || t.is("DEFAULT") {
			if kind == "INSERT" || kind == "REPLACE" || kind == "UPSERT" {
				sawInsertValues = true
			}
		}
		if !commandStart(t) {
			continue
		}
		allowed := false
		switch {
		case t.is("START"):
			allowed = ddlObject == "SEQUENCE" && next.is("WITH") || kind == "SYSTEM" && i == at+1
		case t.is("FLUSH"):
			allowed = kind == "SYSTEM" && i == at+1
		case t.is("RETURN"):
			allowed = ddlObject == "PROCEDURE" || ddlObject == "FUNCTION"
		case t.is("SELECT"):
			setOperator := commandIs(prev, "UNION", "EXCEPT", "INTERSECT", "MINUS") || commandIs(prev, "ALL", "DISTINCT") && i >= 2 && commandIs(ts[i-2], "UNION", "EXCEPT", "INTERSECT", "MINUS")
			allowed = (kind == "SELECT" || sawSourceSelect) && setOperator ||
				(kind == "INSERT" || kind == "REPLACE" || kind == "UPSERT") && !sawSourceSelect && !sawInsertValues ||
				(kind == "CREATE" || kind == "ALTER") && !sawSourceSelect && prev.is("AS") && (ddlObject == "TABLE" || ddlObject == "VIEW") ||
				kind == "EXPLAIN" && !sawSourceSelect
			if allowed {
				sawSourceSelect = true
			}
		case t.is("UPDATE"):
			allowed = kind == "SELECT" && prev.is("FOR") || kind == "MERGE" && prev.is("THEN") ||
				kind == "ALTER" && d == ClickHouse && ddlObject == "TABLE" && !alterMutation ||
				(kind == "CREATE" || kind == "ALTER") && prev.is("ON") && commandIs(next, "CASCADE", "SET", "NO", "RESTRICT") ||
				(kind == "INSERT" || kind == "REPLACE") && IsMySQLDialect(d) && i >= 3 && prev.is("KEY") && ts[i-2].is("DUPLICATE") && ts[i-3].is("ON")
			if kind == "MERGE" && allowed {
				mergeAction = "UPDATE"
				sawSet = false
			}
			if kind == "ALTER" && d == ClickHouse && allowed {
				alterMutation = true
			}
		case t.is("DELETE"), t.is("INSERT"):
			allowed = kind == "MERGE" && prev.is("THEN") || kind == "ALTER" && d == ClickHouse && ddlObject == "TABLE" && !alterMutation ||
				(kind == "CREATE" || kind == "ALTER") && prev.is("ON") && commandIs(next, "CASCADE", "SET", "NO", "RESTRICT")
			if kind == "MERGE" && allowed {
				mergeAction = strings.ToUpper(t.value)
				sawSet = false
			}
			if kind == "ALTER" && d == ClickHouse && allowed {
				alterMutation = true
			}
		case t.is("SET"):
			allowed = (kind == "UPDATE" || kind == "MERGE" && mergeAction == "UPDATE") && !sawSet ||
				kind == "ALTER" && ddlObject == "TABLE" && commandIs(next, "NOT", "DEFAULT", "DATA")
			if allowed {
				sawSet = true
			}
		case t.is("ALTER"):
			allowed = kind == "ALTER" && ddlObject == "TABLE" && next.is("COLUMN")
		case t.is("DROP"):
			allowed = kind == "ALTER" && ddlObject == "TABLE" && (commandIs(next, "COLUMN", "CONSTRAINT") ||
				commandIs(next, "NOT", "DEFAULT") && i >= 3 && ts[i-2].is("COLUMN") && ts[i-3].is("ALTER"))
		case t.is("RENAME"):
			allowed = kind == "ALTER" && ddlObject == "TABLE" && next.is("COLUMN")
		case t.is("REPLACE"):
			allowed = kind == "CREATE" && prev.is("OR") || next.kind == '('
		case t.is("COMMIT"):
			allowed = kind == "CREATE" && ddlObject == "TABLE" && prev.is("ON")
		case t.is("SAVEPOINT"):
			allowed = kind == "ROLLBACK" && prev.is("TO") || kind == "RELEASE"
		case t.is("DESC"):
			allowed = kind == "SELECT" || sawSourceSelect
		case t.is("IF"):
			allowed = (kind == "DROP" || kind == "CREATE") && commandIs(prev, "TABLE", "INDEX", "VIEW") && commandIs(next, "NOT", "EXISTS")
		case t.is("FETCH"):
			allowed = (kind == "SELECT" || sawSourceSelect) && boundedFetchClause(ts, i)
		}
		if !allowed {
			return boundary(t)
		}
	}
	return nil
}

// Privilege verbs are grammar within GRANT/REVOKE, not implicit SQL batches.
// Require the recipient clause and reject unrelated command heads anywhere in
// the unquoted source; the server validates product-specific privilege names.
func validatePrivilegeCommand(ts []token) error {
	recipient, beforeTarget := false, true
	for i, t := range ts {
		if i == 0 || t.quoted || t.kind != 'i' {
			continue
		}
		if commandIs(t, "ON", "TO", "FROM") {
			beforeTarget = false
		}
		if commandIs(t, "TO", "FROM") {
			recipient = true
		}
		if !commandStart(t) {
			continue
		}
		if beforeTarget && commandIs(t, "SELECT", "INSERT", "UPDATE", "DELETE", "CREATE", "ALTER", "DROP", "EXECUTE", "TRUNCATE", "COMMENT") {
			continue
		}
		if t.is("GRANT") && i > 0 && ts[i-1].is("WITH") {
			continue
		}
		if t.is("GRANT") && i == 1 && ts[0].is("REVOKE") {
			continue
		}
		return fmt.Errorf("ambiguous privilege-command boundary near %s; separate SQL statements with semicolons", t.value)
	}
	if !recipient {
		return fmt.Errorf("privilege command requires TO or FROM")
	}
	return nil
}

func boundedFetchClause(ts []token, at int) bool {
	if at+1 >= len(ts) || !commandIs(ts[at+1], "FIRST", "NEXT") {
		return false
	}
	end := at + 2
	for end < len(ts) && ts[end].kind >= '0' && ts[end].kind <= '9' {
		end++
	}
	if end == at+2 || end+1 >= len(ts) || !commandIs(ts[end], "ROW", "ROWS") || !ts[end+1].is("ONLY") {
		return false
	}
	return true
}

func standaloneDirective(document string, t token) bool {
	start, end := strings.LastIndexByte(document[:t.start], '\n')+1, len(document)
	if n := strings.IndexByte(document[t.end:], '\n'); n >= 0 {
		end = t.end + n
	}
	line := strings.TrimSpace(document[start:end])
	if len(line) < len(t.text) || !strings.EqualFold(line[:len(t.text)], t.text) {
		return false
	}
	rest := strings.TrimSpace(line[len(t.text):])
	return rest == "" || strings.HasPrefix(rest, "--") || rest[0] >= '0' && rest[0] <= '9'
}
