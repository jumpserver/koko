package engine

import "net/http"

// Cached rows belong to a displayed result, not to every execution ever made in
// a session. The UI releases superseded results after their replacement loads.
// Accepted request identities remain intact so a delayed retry never writes twice.
func (v *Server) deleteResult(w http.ResponseWriter, r *http.Request) {
	s := v.auth(w, r, true)
	if s == nil {
		return
	}
	s.mu.Lock()
	s.releaseResultLocked(r.PathValue("rid"))
	s.mu.Unlock()
	w.WriteHeader(http.StatusNoContent)
}

func (s *session) releaseResultLocked(id string) {
	if result := s.results[id]; result != nil {
		s.resultBytes -= result.memoryBytes
		delete(s.results, id)
		for key, plan := range s.editPlans {
			if plan.resultID == id && !plan.used {
				delete(s.editPlans, key)
			}
		}
	}
}

// The worker calls this after all accepted operations have drained, including
// canceled queued work. Closing a tab must release its row budget for other tabs.
func (s *session) releaseClosedConsoleResults(c *console) {
	if !c.closed.Load() {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, result := range s.results {
		if result.consoleID == c.id {
			s.releaseResultLocked(id)
		}
	}
	for id, execution := range s.executions {
		if execution.view.ConsoleID == c.id {
			execution.mu.Lock()
			if execution.planResult != nil {
				s.resultBytes -= execution.planResult.memoryBytes
				execution.planResult = nil
			}
			execution.mu.Unlock()
			delete(s.executions, id)
		}
	}
}

// A completed history entry must not consume the concurrency allowance. Keep
// its identity for idempotent retry recovery until the console/session closes.
func (s *session) activeExecutionCountLocked() int {
	count := 0
	for _, execution := range s.executions {
		execution.mu.RLock()
		if !terminal(execution.view.Status) {
			count++
		}
		execution.mu.RUnlock()
	}
	return count
}
