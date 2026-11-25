package engine

import (
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/jumpserver/koko/pkg/chen/sqlservice"
)

func (c *console) currentContext() sqlservice.Context {
	if selected := c.selectedContext.Load(); selected != nil {
		return *selected
	}
	return c.context
}

// Caller holds console.mu; snapshots also have lock-free readers.
func (c *console) publishContext(s *session, scope sqlservice.Context) {
	if scope != c.currentContext() {
		c.selectedContext.Store(&scope)
		s.emit("console.updated", c.view())
	}
}

func (v *Server) consoleContexts(w http.ResponseWriter, r *http.Request) {
	s, c, ctx, done := v.catalogConsole(w, r)
	if s == nil {
		return
	}
	defer done()
	after := r.URL.Query().Get("after")
	if !utf8.ValidString(after) || utf8.RuneCountInString(after) > 256 || strings.ContainsRune(after, 0) {
		fail(w, 400, "invalid_catalog_request", "Invalid context cursor")
		return
	}
	if v.auditCatalog(ctx, s, c, "contexts") != nil {
		fail(w, 503, "audit_persistence_failed", "Context access could not be audited")
		return
	}
	dialect := s.grant.Config.Dialect
	scope, err := liveContext(ctx, c, dialect)
	if err != nil {
		catalogError(w, r, s, err)
		return
	}
	c.publishContext(s, scope)
	const limit = 100
	names, err := sqlservice.ConsoleContextNames(ctx, c.conn, sqlservice.Dialect(dialect), scope.Database, after, limit)
	if err != nil {
		catalogError(w, r, s, err)
		return
	}
	page := catalogPage[string]{Items: names}
	if len(names) > limit {
		page.Items, page.HasMore, page.NextCursor = names[:limit], true, names[limit-1]
	}
	writeJSON(w, 200, page)
}
