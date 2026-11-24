package agentexec

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jumpserver-dev/sdk-go/model"
	"github.com/jumpserver/koko/pkg/session"
)

// Check every live session: idle, queued and draining SQL sessions may still
// own a transaction before an execution guard starts. Rotate a bounded batch
// so one slow Core request does not prevent others from observing revocation.
func (r *Runtime) checkSessionAuthorities(parent context.Context) {
	ctx, cancel := context.WithTimeout(parent, 5*time.Second)
	defer cancel()
	r.mu.Lock()
	count := min(len(r.sessions), 16)
	r.mu.Unlock()
	for range count {
		if ctx.Err() != nil {
			return
		}
		s := r.nextSession(&r.authorityNext)
		if s == nil {
			return
		}
		s.mu.Lock()
		info, releasing := s.info, s.closingNow
		s.mu.Unlock()
		if releasing {
			continue
		}
		checkCtx, stop := context.WithTimeout(ctx, time.Second)
		err := r.sessionCall(checkCtx, info, "check", nil, &Session{})
		stop()
		var denied *CoreError
		if errors.As(err, &denied) && (denied.Status == 401 || denied.Status == 403 || denied.Status == 404) {
			r.stopControlledSession(s, nil)
			r.closeSession(s)
		}
		// A transient outage does not prove revocation. Preserve the transport;
		// new operations still require Core authorization before dispatch.
	}
}

// registerSessionControl makes the real terminal session visible to Koko's
// heartbeat, administrator tasks and connection-token permission checks. The
// caller supplies TokenId explicitly when Core's session response omits it,
// and removes the registration when closeSession releases the session.
func (r *Runtime) registerSessionControl(s *liveSession, terminal model.Session) {
	previous, exists := session.GetSessionById(terminal.ID)
	if exists {
		// SFTP already owns registration and lifecycle audit. Preserve its
		// original token metadata and delegate the existing control handler.
		terminal = *previous.Session
	}
	controlled := session.NewSession(&terminal, func(task *model.TerminalTask) error {
		switch task.Name {
		case model.TaskKillSession, model.TaskPermExpired, model.TaskLockSession:
			var previousStop func()
			if exists && task.Name != model.TaskLockSession {
				previousStop = func() { _ = previous.HandleTask(task) }
			}
			r.stopControlledSession(s, previousStop)
			return nil
		case model.TaskPermValid:
			if exists {
				return previous.HandleTask(task)
			}
			return nil
		case model.TaskUnlockSession:
			// A closed SSH/DB transaction context cannot be reconstructed or
			// silently resumed. The user must establish a new session.
			return nil
		default:
			if exists {
				return previous.HandleTask(task)
			}
			return fmt.Errorf("agent session does not support task %s", task.Name)
		}
	})
	session.AddSession(controlled)
}

func (r *Runtime) stopControlledSession(s *liveSession, previousStop func()) {
	s.mu.Lock()
	s.closing = true
	s.info.Status = "context_lost"
	id, connection, cancelConnect := s.info.ID, s.connection, s.connectCancel
	s.mu.Unlock()
	// Let SFTP's own CloseWithReason take its lifecycle lock before cancelling
	// I/O, so its existing administrator/permission-expired reason is retained.
	if previousStop != nil {
		previousStop()
	}
	if cancelConnect != nil {
		cancelConnect()
	}
	r.mu.Lock()
	jobs := make(map[string]context.CancelFunc, len(r.jobs))
	for actionID, cancel := range r.jobs {
		jobs[actionID] = cancel
	}
	r.mu.Unlock()
	for actionID, cancel := range jobs {
		if rec, err := r.journal.Action(actionID); err == nil && rec.Action.SessionID == id {
			cancel()
		}
	}
	if connection != nil {
		_ = connection.Close()
	}
	// Workers retain ownership of their final evidence. Closing the transport
	// never fabricates a confirmed remote exit or a successful rollback.
}
