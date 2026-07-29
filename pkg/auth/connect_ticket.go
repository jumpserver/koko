package auth

import (
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/jumpserver-dev/sdk-go/model"
	"github.com/jumpserver/koko/pkg/common"
)

const (
	connectTicketHeader      = "X-Koko-Connect-Ticket"
	connectTicketQuery       = "ticket"
	connectTicketDefaultTTL  = 30 * time.Minute
	connectTicketCleanupStep = time.Minute
)

type ConnectTicket struct {
	ID        string
	User      *model.User
	Headers   map[string]string
	TokenID   string
	OrgID     string
	ExpiresAt time.Time
}

type ConnectTicketStore struct {
	mu    sync.RWMutex
	items map[string]*ConnectTicket
}

func NewConnectTicketStore() *ConnectTicketStore {
	store := &ConnectTicketStore{
		items: make(map[string]*ConnectTicket),
	}
	go store.runGC()
	return store
}

func (s *ConnectTicketStore) Create(user *model.User, headers map[string]string, tokenID string, orgID string) *ConnectTicket {
	ticket := &ConnectTicket{
		ID:        common.UUID(),
		User:      cloneUser(user),
		Headers:   cloneHeaders(headers),
		TokenID:   strings.TrimSpace(tokenID),
		OrgID:     strings.TrimSpace(orgID),
		ExpiresAt: time.Now().Add(connectTicketDefaultTTL),
	}

	s.mu.Lock()
	s.items[ticket.ID] = ticket
	s.mu.Unlock()

	return ticket
}

func (s *ConnectTicketStore) Get(ticketID string) (*ConnectTicket, bool) {
	if ticketID == "" {
		return nil, false
	}

	now := time.Now()

	s.mu.RLock()
	ticket, ok := s.items[ticketID]
	s.mu.RUnlock()
	if !ok {
		return nil, false
	}
	if now.After(ticket.ExpiresAt) {
		s.mu.Lock()
		delete(s.items, ticketID)
		s.mu.Unlock()
		return nil, false
	}

	return ticket, true
}

func (s *ConnectTicketStore) runGC() {
	ticker := time.NewTicker(connectTicketCleanupStep)
	defer ticker.Stop()

	for range ticker.C {
		now := time.Now()
		s.mu.Lock()
		for id, ticket := range s.items {
			if now.After(ticket.ExpiresAt) {
				delete(s.items, id)
			}
		}
		s.mu.Unlock()
	}
}

func RequestConnectTicket(req *http.Request) string {
	if req == nil {
		return ""
	}
	if ticket := strings.TrimSpace(req.Header.Get(connectTicketHeader)); ticket != "" {
		return ticket
	}
	return strings.TrimSpace(req.URL.Query().Get(connectTicketQuery))
}

func cloneHeaders(headers map[string]string) map[string]string {
	if len(headers) == 0 {
		return map[string]string{}
	}
	cloned := make(map[string]string, len(headers))
	for key, value := range headers {
		cloned[key] = value
	}
	return cloned
}

func cloneUser(user *model.User) *model.User {
	if user == nil {
		return nil
	}
	cloned := *user
	return &cloned
}

var ConnectTickets = NewConnectTicketStore()
