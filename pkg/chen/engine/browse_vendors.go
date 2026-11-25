package engine

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/jumpserver/koko/pkg/chen/dialect"
	"github.com/jumpserver/koko/pkg/chen/sqlservice"
)

// SQL Server paging projects each visible column to exclude the synthetic row
// number. An explicit refresh must rebuild that projection in the result's
// original database, even when the console has since executed USE.
func refreshSQLServerBrowseTable(ctx context.Context, s *session, c *console, p *browsePlan) (err error) {
	guard, err := prepareSourceReadContext(ctx, c, dialect.SQLServer, p.context)
	if err != nil {
		return err
	}
	if guard != nil {
		defer func() {
			if restoreErr := guard.restore(s); restoreErr != nil {
				err = restoreErr
			}
		}()
		if err := guard.enter(ctx); err != nil {
			return err
		}
	}
	scope := sqlservice.Context{Database: p.view.Database, Schema: p.view.Schema}
	table, err := sqlservice.LoadBrowseTable(ctx, c.conn, dialect.SQLServer, scope, scope.Schema, p.view.Table)
	if err != nil {
		return errors.New("Unable to refresh table column metadata")
	}
	if table.View != p.table.View || !slices.Equal(table.PrimaryKey, p.table.PrimaryKey) {
		return &APIError{"browse_source_changed", "The table or view changed; reopen it before browsing"}
	}
	p.table = table
	return nil
}

func browsePolicyLiteral(d dialect.ID, text string) (string, error) {
	if d == dialect.Oracle && text == "" {
		return "''", nil
	}
	if d == dialect.ClickHouse {
		return "'" + strings.ReplaceAll(strings.ReplaceAll(text, "\\", "\\\\"), "'", "\\'") + "'", nil
	}
	if d == dialect.DB2 {
		return "'" + strings.ReplaceAll(text, "'", "''") + "'", nil
	}
	return copyTextLiteral(d, text)
}

// The base/order arguments are built from catalog-validated columns above,
// never found by deleting text from a user's query. Explicit projection removes
// the synthetic row number, including when a real column has the same name.
func browsePageSQL(d dialect.ID, full, base, order string, columns []sqlservice.EditColumn, offset, size int) string {
	if !browsePageProjectsColumns(d) {
		return fmt.Sprintf("%s LIMIT %d OFFSET %d", full, size, offset)
	}
	used := map[string]bool{}
	projection := make([]string, len(columns))
	for i, c := range columns {
		used[strings.ToLower(c.Name)] = true
		projection[i] = quoteDatabaseIdentifier(c.Name, d)
	}
	alias := "CHEN_row_number"
	for i := 1; used[strings.ToLower(alias)]; i++ {
		alias = fmt.Sprintf("CHEN_row_number_%d", i)
	}
	rn := quoteDatabaseIdentifier(alias, d)
	if d == dialect.Oracle || d == dialect.OceanBaseOracle {
		return fmt.Sprintf("SELECT %s FROM (SELECT CHEN_page.*, ROWNUM %s FROM (\n%s\n) CHEN_page WHERE ROWNUM<=%d) WHERE %s>%d ORDER BY %s", strings.Join(projection, ","), rn, full, offset+size, rn, offset, rn)
	}
	window := ""
	if order != "" {
		window = "ORDER BY " + order
	} else if d == dialect.SQLServer {
		window = "ORDER BY (SELECT NULL)"
	}
	// ROW_NUMBER is available before OFFSET/FETCH (SQL Server 2005+, DB2 9.7+).
	// No synthetic field reaches the result or its row references.
	return fmt.Sprintf("SELECT %s FROM (SELECT CHEN_source.*, ROW_NUMBER() OVER (%s) AS %s FROM (\n%s\n) CHEN_source) AS CHEN_page WHERE %s>%d AND %s<=%d ORDER BY %s", strings.Join(projection, ","), window, rn, base, rn, offset, rn, offset+size, rn)
}

func browsePageProjectsColumns(d dialect.ID) bool {
	return d == dialect.Oracle || d == dialect.OceanBaseOracle || d == dialect.SQLServer || d == dialect.DB2
}
