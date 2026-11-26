package service

import (
	"context"
	"errors"
	"time"

	"github.com/jumpserver/koko/internal/kael/domain"
	"github.com/jumpserver/koko/internal/kael/identity"
	"github.com/jumpserver/koko/internal/kael/ports"
)

// Approval passwords live only in this process until the waiting call takes
// them, and are never written to the journal or audit store.
func (s *Service) keepAccountPassword(approvalID, password string) {
	s.accountPasswordsMu.Lock()
	if s.accountPasswords == nil {
		s.accountPasswords = make(map[string]string)
	}
	s.accountPasswords[approvalID] = password
	s.accountPasswordsMu.Unlock()
	time.AfterFunc(10*time.Minute, func() {
		s.accountPasswordsMu.Lock()
		delete(s.accountPasswords, approvalID)
		s.accountPasswordsMu.Unlock()
	})
}

func (s *Service) takeAccountPassword(approvalID string) string {
	s.accountPasswordsMu.Lock()
	defer s.accountPasswordsMu.Unlock()
	password := s.accountPasswords[approvalID]
	delete(s.accountPasswords, approvalID)
	return password
}

func (s *Service) keepSecretInputs(approvalID string, inputs map[string]string) {
	if len(inputs) == 0 {
		return
	}
	copyOfInputs := make(map[string]string, len(inputs))
	for name, value := range inputs {
		copyOfInputs[name] = value
	}
	s.accountPasswordsMu.Lock()
	if s.approvalSecrets == nil {
		s.approvalSecrets = make(map[string]map[string]string)
	}
	s.approvalSecrets[approvalID] = copyOfInputs
	s.accountPasswordsMu.Unlock()
	time.AfterFunc(10*time.Minute, func() {
		s.accountPasswordsMu.Lock()
		delete(s.approvalSecrets, approvalID)
		s.accountPasswordsMu.Unlock()
	})
}

func (s *Service) takeSecretInputs(approvalID string) map[string]string {
	s.accountPasswordsMu.Lock()
	defer s.accountPasswordsMu.Unlock()
	inputs := s.approvalSecrets[approvalID]
	delete(s.approvalSecrets, approvalID)
	return inputs
}

// The caller holds credentialsMu across queueing and binding so a worker
// cannot execute a committed run before its request credentials are available.
func (s *Service) bindRunCredentials(ctx context.Context, run *domain.Run) {
	if run.CapabilityMode != "service" {
		return
	}
	if s.runCredentials == nil {
		s.runCredentials = make(map[string]identity.CoreCredentials)
	}
	s.runCredentials[run.ID] = identity.CoreCredentialsFromContext(ctx)
}

func (s *Service) capabilityContext(ctx context.Context, runID string) context.Context {
	s.credentialsMu.Lock()
	credentials := s.runCredentials[runID]
	s.credentialsMu.Unlock()
	return identity.WithCoreCredentials(ctx, credentials)
}

func (s *Service) forgetRunCredentials(runID string) {
	s.credentialsMu.Lock()
	delete(s.runCredentials, runID)
	s.credentialsMu.Unlock()
}

func (s *Service) pruneRunCredentials(ctx context.Context) error {
	s.credentialsMu.Lock()
	defer s.credentialsMu.Unlock()
	if len(s.runCredentials) == 0 {
		return nil
	}
	return s.store.View(ctx, func(tx ports.Tx) error {
		for id := range s.runCredentials {
			run, err := tx.RunInternal(id, false)
			if errors.Is(err, ports.ErrNotFound) || err == nil && run.Terminal() {
				delete(s.runCredentials, id)
			} else if err != nil {
				return err
			}
		}
		return nil
	})
}
