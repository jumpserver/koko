package webproxy

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/jumpserver-dev/sdk-go/model"
	"github.com/jumpserver/koko/pkg/auth"
	"github.com/jumpserver/koko/pkg/session"
)

// A browser uses multiple HTTP connections. Its heartbeat keeps the logical
// session alive; a crashed client must not leave a reusable proxy session behind.
const proxySessionIdleTimeout = 5 * time.Minute

type proxySession struct {
	id       string
	ticket   string
	ctx      context.Context
	cancel   context.CancelFunc
	locked   bool
	lastSeen time.Time
	timer    *time.Timer
}

type proxyAuth struct {
	mu       sync.Mutex
	sessions map[string]*proxySession
}

func validatedProxyTicket(r *http.Request, tokenID string) (*auth.ConnectTicket, bool) {
	ticket, ok := auth.ConnectTickets.Get(r.Header.Get("X-Koko-Connect-Ticket"))
	return ticket, ok && ticket.User != nil && ticket.User.ID != "" && ticket.TokenID != "" && ticket.TokenID == tokenID
}

func (s *Server) registerProxySession(apiSession *model.Session, ticketID string) error {
	s.auth.mu.Lock()
	defer s.auth.mu.Unlock()
	if len(s.auth.sessions) >= maxCredentialSessions {
		return errors.New("too many Web proxy sessions")
	}
	ctx, cancel := context.WithCancel(context.Background())
	current := &proxySession{id: apiSession.ID, ticket: ticketID, ctx: ctx, cancel: cancel, lastSeen: time.Now()}
	s.auth.sessions[current.id] = current
	var expire func()
	expire = func() {
		s.auth.mu.Lock()
		if s.auth.sessions[current.id] != current {
			s.auth.mu.Unlock()
			return
		}
		remaining := time.Until(current.lastSeen.Add(proxySessionIdleTimeout))
		if remaining > 0 {
			current.timer = time.AfterFunc(remaining, expire)
			s.auth.mu.Unlock()
			return
		}
		s.auth.mu.Unlock()
		_ = s.closeProxySession(current.id)
	}
	current.timer = time.AfterFunc(proxySessionIdleTimeout, expire)
	// Match SSH/Lion: ticket authorizes establishment, while the shared session
	// registry handles permissions and administrative tasks after establishment.
	session.AddSession(session.NewSession(apiSession, func(task *model.TerminalTask) error {
		switch task.Name {
		case model.TaskKillSession, model.TaskPermExpired:
			return s.closeProxySession(current.id)
		case model.TaskPermValid:
			return nil
		case model.TaskLockSession, model.TaskUnlockSession:
			s.auth.mu.Lock()
			defer s.auth.mu.Unlock()
			if s.auth.sessions[current.id] != current {
				return nil
			}
			if task.Name == model.TaskLockSession {
				current.locked = true
				current.cancel()
			} else if current.locked {
				current.locked = false
				current.ctx, current.cancel = context.WithCancel(context.Background())
			}
			return nil
		default:
			return fmt.Errorf("Web proxy session unknown task %s", task.Name)
		}
	}))
	return nil
}

func (s *Server) authenticateProxy(r *http.Request) *proxySession {
	scheme, encoded, ok := strings.Cut(r.Header.Get("Proxy-Authorization"), " ")
	if !ok || !strings.EqualFold(scheme, "Basic") || len(encoded) > 1024 {
		return nil
	}
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil
	}
	id, ticket, ok := strings.Cut(string(decoded), ":")
	if !ok || ticket == "" {
		return nil
	}
	s.auth.mu.Lock()
	defer s.auth.mu.Unlock()
	current := s.auth.sessions[id]
	if current == nil || !secureEqual(current.ticket, ticket) || time.Since(current.lastSeen) >= proxySessionIdleTimeout {
		return nil
	}
	// Heartbeat and close must also work while an administrator locks the session.
	if current.locked && r.URL.IsAbs() {
		return nil
	}
	current.lastSeen = time.Now()
	snapshot := *current
	return &snapshot
}

func (s *Server) closeProxySession(id string) error {
	s.auth.mu.Lock()
	current := s.auth.sessions[id]
	if current != nil {
		delete(s.auth.sessions, id)
		current.cancel()
		current.timer.Stop()
	}
	s.auth.mu.Unlock()
	if current == nil {
		return nil
	}
	session.RemoveSessionById(id)
	auth.ConnectTickets.Delete(current.ticket)
	s.credentials.mu.Lock()
	for credentialID, credential := range s.credentials.sessions {
		if credential.proxySessionID == id {
			delete(s.credentials.sessions, credentialID)
		}
	}
	s.credentials.mu.Unlock()
	if s.coreService != nil {
		_, err := s.coreService.SessionDisconnect(id)
		s.finishSessionRecordings(id)
		return err
	}
	return nil
}

func (s *Server) stopProxySessions() {
	s.auth.mu.Lock()
	ids := make([]string, 0, len(s.auth.sessions))
	for id := range s.auth.sessions {
		ids = append(ids, id)
	}
	s.auth.mu.Unlock()
	for _, id := range ids {
		_ = s.closeProxySession(id)
	}
}

func requireProxyAuth(w http.ResponseWriter) {
	w.Header().Set("Proxy-Authenticate", `Basic realm="JumpServer Web Proxy"`)
	http.Error(w, "proxy authentication required", http.StatusProxyAuthRequired)
}
