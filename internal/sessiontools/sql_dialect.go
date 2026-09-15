package sessiontools

import "strings"

// SQL dialect differences belong here so validation and background execution
// use the same statement boundaries and execution policy.
type sqlDialect struct {
	read, write, session string
	mysql                bool
	postgres             bool
	sqlserver            bool
	oracle               bool
	clickhouse           bool
}

func dialectForSQL(protocol string) sqlDialect {
	switch strings.ToLower(strings.TrimSpace(protocol)) {
	case "mysql", "mariadb":
		return sqlDialect{
			mysql:   true,
			read:    " SHOW DESC DESCRIBE ",
			write:   " REPLACE RENAME ANALYZE OPTIMIZE REPAIR LOAD KILL SHUTDOWN RESET PURGE FLUSH INSTALL UNINSTALL ",
			session: " USE START RELEASE LOCK UNLOCK CALL PREPARE EXECUTE DEALLOCATE DO ",
		}
	case "postgresql":
		return sqlDialect{
			postgres: true,
			read:     " SHOW VALUES TABLE ",
			write:    " MERGE ANALYZE VACUUM REINDEX REFRESH COMMENT ",
			session:  " START RELEASE LOCK CALL PREPARE EXECUTE DEALLOCATE RESET DISCARD DO DECLARE FETCH MOVE CLOSE LISTEN UNLISTEN NOTIFY ",
		}
	case "sqlserver":
		return sqlDialect{
			sqlserver: true, write: " MERGE ",
			session: " USE EXEC EXECUTE DECLARE DBCC ",
		}
	case "oracle":
		return sqlDialect{
			oracle: true, read: " DESC DESCRIBE ",
			write:   " MERGE ANALYZE RENAME COMMENT PURGE ",
			session: " CALL EXEC EXECUTE LOCK ",
		}
	case "clickhouse":
		return sqlDialect{
			clickhouse: true, read: " SHOW DESC DESCRIBE EXISTS CHECK ",
			write:   " RENAME OPTIMIZE ATTACH DETACH SYSTEM KILL ",
			session: " USE ",
		}
	default:
		return sqlDialect{}
	}
}

func (d sqlDialect) keywordKind(keyword string) sqlKind {
	switch keyword {
	case "SELECT", "EXPLAIN":
		return sqlRead
	case "INSERT", "UPDATE", "DELETE", "CREATE", "ALTER", "DROP", "TRUNCATE", "GRANT", "REVOKE":
		return sqlWrite
	case "SET", "BEGIN", "COMMIT", "ROLLBACK", "SAVEPOINT":
		return sqlSession
	}
	word := " " + keyword + " "
	switch {
	case keyword == "":
		return sqlUnknown
	case strings.Contains(d.read, word):
		return sqlRead
	case strings.Contains(d.write, word):
		return sqlWrite
	case strings.Contains(d.session, word):
		return sqlSession
	default:
		return sqlUnknown
	}
}

func sqlDollarDelimiter(statement string, index int) string {
	for end := index + 1; end < len(statement); end++ {
		value := statement[end]
		if value == '$' {
			return statement[index : end+1]
		}
		if value != '_' && !(value >= 'a' && value <= 'z') && !(value >= 'A' && value <= 'Z') &&
			!(end > index+1 && value >= '0' && value <= '9') {
			break
		}
	}
	return ""
}
