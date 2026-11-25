package sqlservice

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/pingcap/tidb/pkg/parser"
	"github.com/pingcap/tidb/pkg/parser/ast"
	"github.com/pingcap/tidb/pkg/parser/mysql"
)

// ManualTransactionRisk is evaluated after native parsing, using the same live
// SQL options at admission and execution. Unknown/procedural commands fail closed:
// a routine can contain transaction boundaries invisible to its CALL statement.
func ManualTransactionRisk(d Dialect, s Statement) (implicit, control bool) {
	switch s.Kind {
	case "transaction", "begin", "commit", "rollback", "savepoint", "release", "start", "save", "set", "variableset", "session", "use", "settransaction":
		return false, true
	case "select", "setopr", "insert", "update", "delete", "merge", "replace", "show", "variableshow":
		return false, false
	}
	// PostgreSQL and SQL Server reject transaction-breaking DDL in an explicit
	// transaction. Keep their DDL inside it; never pre-commit transactional DDL.
	if d == PostgreSQL || d == SQLServer {
		if s.Kind == "ddl" || strings.HasPrefix(s.Kind, "create") || strings.HasPrefix(s.Kind, "alter") || strings.HasPrefix(s.Kind, "drop") || s.Kind == "truncate" || s.Kind == "index" || s.Kind == "view" || s.Kind == "grant" || s.Kind == "comment" {
			return false, false
		}
	}
	// Db2 has DDL exceptions (for example TRUNCATE IMMEDIATE) outside rollback
	// control. Its bounded parser cannot prove the variant, so require consent.
	return true, false
}

type transactionTables struct{ tables []*ast.TableName }

func (v *transactionTables) Enter(n ast.Node) (ast.Node, bool) {
	if t, ok := n.(*ast.TableName); ok {
		v.tables = append(v.tables, t)
	}
	return n, false
}
func (v *transactionTables) Leave(n ast.Node) (ast.Node, bool) { return n, true }

// Resolve each table on the actual console connection. information_schema cannot
// distinguish a temporary table shadowing a permanent table of the same name,
// and USE may have changed its default database before manual mode was enabled.
func CheckTransactionTables(ctx context.Context, conn *sql.Conn, d Dialect, s Statement, opts AnalyzeOptions) error {
	if !IsMySQLDialect(d) || !s.MayWrite {
		return nil
	}
	input, err := mysqlParserInput(s.SQL, opts)
	if err != nil {
		return err
	}
	mode, err := mysql.GetSQLMode(opts.MySQLSQLMode)
	if err != nil {
		return err
	}
	p := parser.New()
	p.SetSQLMode(mode)
	nodes, _, err := p.ParseSQL(input)
	if err != nil {
		return err
	}
	visitor := &transactionTables{}
	for _, node := range nodes {
		node.Accept(visitor)
	}
	if d == OceanBaseMySQL && len(visitor.tables) != 0 {
		var tenantMode string
		if err := conn.QueryRowContext(ctx, "SELECT @@ob_compatibility_mode").Scan(&tenantMode); err != nil {
			return err
		}
		if !strings.EqualFold(tenantMode, "MYSQL") {
			return fmt.Errorf("cannot verify OceanBase MySQL tenant")
		}
	}
	seen := map[string]bool{}
	for _, table := range visitor.tables {
		key := quoteIdentifier(table.Name.O, d, '`')
		if table.Schema.O != "" {
			key = quoteIdentifier(table.Schema.O, d, '`') + "." + key
		}
		if seen[key] {
			continue
		}
		seen[key] = true
		var name, definition string
		if err := conn.QueryRowContext(ctx, "SHOW CREATE TABLE "+key).Scan(&name, &definition); err != nil {
			return err
		}
		if d == OceanBaseMySQL {
			// OceanBase base tables use its transactional storage engine. Its
			// server-generated DDL includes native options in place of ENGINE.
			words := strings.Fields(definition)
			if len(words) < 3 || !strings.EqualFold(words[0], "CREATE") || !strings.EqualFold(words[1], "TABLE") {
				return fmt.Errorf("cannot verify transactional base table")
			}
			continue
		}
		node, err := p.ParseOneStmt(definition, "", "")
		if err != nil {
			return err
		}
		create, ok := node.(*ast.CreateTableStmt)
		verified := false
		if ok {
			for _, option := range create.Options {
				if option.Tp == ast.TableOptionEngine {
					verified = strings.EqualFold(option.StrValue, "InnoDB")
					break
				}
			}
		}
		if !verified {
			return fmt.Errorf("cannot verify transactional table engine")
		}
	}
	return nil
}
