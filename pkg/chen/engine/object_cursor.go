package engine

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/jumpserver/koko/pkg/chen/dialect"
	"github.com/jumpserver/koko/pkg/chen/sqlservice"
)

type cursorCell struct {
	Row     int `json:"row"`
	Column  int `json:"column"`
	name    string
	created time.Time
	role    string
}
type cursorSource struct {
	Ref   sqlservice.ObjectRef `json:"ref"`
	OID   uint32               `json:"oid"`
	Cells []cursorCell         `json:"cells"`
}

// Only the driver-reported built-in refcursor OID can publish a portal handle.
// Bind visible, unmasked cells to their portals before releasing the console.
func bindObjectCursors(s *session, e *execution, results []*Result) {
	if e.objectPlan == nil || e.objectPlan.req.Action != "invoke" || s.grant.Dialect() != dialect.PostgreSQL {
		return
	}
	s.mu.Lock()
	c := s.consoles[e.view.ConsoleID]
	s.mu.Unlock()
	if c == nil || c.manualTx == nil {
		return
	}
	type binding struct {
		result *Result
		cell   cursorCell
	}
	var cells []binding
	var names []string
	for _, r := range results {
		for col, field := range r.Columns {
			if field.DataType != "1790" && field.DataType != "REFCURSOR" {
				continue
			}
			for row, values := range r.Rows {
				name, valid := values[col].(string)
				if !valid || name == "" || len(name) > 63 || r.masked[row][col] {
					continue
				}
				if len(cells) == 100 {
					break
				}
				cells = append(cells, binding{r, cursorCell{Row: row, Column: col, name: name}})
				names = append(names, name)
			}
		}
	}
	if len(cells) == 0 {
		return
	}
	var allowed bool
	if err := c.conn.QueryRowContext(e.ctx, `SELECT pg_catalog.has_table_privilege('pg_catalog.pg_cursors','SELECT')`).Scan(&allowed); err != nil || !allowed {
		return
	}
	rows, err := c.conn.QueryContext(e.ctx, `SELECT name,creation_time,current_user FROM pg_catalog.pg_cursors WHERE name=ANY($1::pg_catalog.text[])`, names)
	if err != nil {
		return
	}
	defer rows.Close()
	created := map[string]time.Time{}
	var role string
	for rows.Next() {
		var name string
		var at time.Time
		if rows.Scan(&name, &at, &role) != nil {
			return
		}
		created[name] = at
	}
	if rows.Err() != nil {
		return
	}
	for _, b := range cells {
		at, ok := created[b.cell.name]
		if !ok {
			continue
		}
		if b.result.CursorSource == nil {
			b.result.CursorSource = &cursorSource{Ref: e.objectPlan.commands.Ref, OID: e.objectPlan.req.OID}
		}
		b.cell.created = at
		b.cell.role = role
		b.result.CursorSource.Cells = append(b.result.CursorSource.Cells, b.cell)
	}
}

func (v *Server) cursorCommands(ctx context.Context, s *session, c *console, req sqlservice.ObjectPlanRequest) (sqlservice.ObjectCommands, error) {
	out := sqlservice.ObjectCommands{Ref: req.Ref}
	cur := req.Cursor
	if s.grant.Dialect() != dialect.PostgreSQL || c.manualTx == nil || cur == nil {
		return out, sqlservice.ErrObjectChanged
	}
	if cur.Limit < 1 || cur.Limit > min(200, v.opts.MaxRows) {
		return out, errors.New("cursor fetch size exceeds the result limit")
	}
	s.mu.Lock()
	r := s.results[cur.ResultID]
	var cell cursorCell
	if r != nil && r.consoleID == c.id && r.CursorSource != nil {
		origin := r.CursorSource.Ref
		if r.CursorSource.OID == req.OID && req.Ref.Database == origin.Database && req.Ref.Schema == origin.Schema && req.Ref.Kind == origin.Kind && req.Ref.Name == origin.Name && req.Ref.Table == origin.Table && slices.Equal(req.Ref.InputTypes, origin.InputTypes) {
			for _, candidate := range r.CursorSource.Cells {
				if candidate.Row == cur.Row && candidate.Column == cur.Column {
					cell = candidate
					break
				}
			}
		}
	}
	s.mu.Unlock()
	if cell.name == "" {
		return out, sqlservice.ErrObjectChanged
	}
	var created time.Time
	var role string
	err := c.conn.QueryRowContext(ctx, `SELECT creation_time,current_user FROM pg_catalog.pg_cursors WHERE name=$1`, cell.name).Scan(&created, &role)
	if errors.Is(err, sql.ErrNoRows) || err == nil && (!created.Equal(cell.created) || role != cell.role) {
		return out, sqlservice.ErrObjectChanged
	}
	if err != nil {
		return out, err
	}
	statement := fmt.Sprintf("FETCH FORWARD %d FROM %s", cur.Limit, quoteDatabaseIdentifier(cell.name, dialect.PostgreSQL))
	out.ExecutionUser = role
	out.Steps = []sqlservice.ObjectStep{{SQL: statement, ResultRole: "cursor_rows"}}
	out.Fingerprint = fmt.Sprintf("%x", sha256.Sum256([]byte(role+"\x00"+created.String()+"\x00"+statement)))
	return out, nil
}
