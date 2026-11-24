package engine

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"
)

func pinnedMySQLCatalog(dialect string) bool {
	return dialect == "mysql" || dialect == "mariadb"
}

// Keep the console locked until the interrupt has completed. Otherwise a late
// KILL QUERY could reach the next user statement on the same physical session.
func interruptCatalogOnCancel(ctx context.Context, s *session, c *console) context.CancelFunc {
	done := make(chan struct{})
	stop := context.AfterFunc(ctx, func() {
		defer close(done)
		interrupt, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := cancelBackend(interrupt, s, c, nil); err != nil {
			slog.Warn("catalog_interrupt_failed", "dialect", s.grant.Config.Dialect, "error", err)
		}
	})
	return func() {
		if !stop() {
			<-done
		}
	}
}

func (v *Server) runAITool(ctx context.Context, r *http.Request, s *session, e aiEditorContext, name string, raw json.RawMessage) (any, error) {
	if e.ConsoleID == "" || !pinnedMySQLCatalog(s.grant.Config.Dialect) || (name != "inspect_schema" && name != "search_schema") {
		return v.executeAITool(ctx, r, s, e, name, raw)
	}
	// Return the tool result on time even if the interrupted driver still has
	// bytes to drain. Its worker owns the console lock and catalog slot until then.
	ctx, cancel := context.WithTimeout(ctx, catalogInspectionTimeout)
	defer cancel()
	type outcome struct {
		value any
		err   error
	}
	completed := make(chan outcome, 1)
	s.workers.Add(1)
	go func() {
		defer s.workers.Done()
		value, err := v.executeAITool(ctx, r, s, e, name, raw)
		completed <- outcome{value, err}
	}()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case result := <-completed:
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		return result.value, result.err
	}
}
