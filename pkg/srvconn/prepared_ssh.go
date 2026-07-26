package srvconn

import (
	"strings"
	"sync"

	"github.com/jumpserver-dev/sdk-go/model"
)

// PreparedSSHClient holds an authenticated asset SSH client until the
// connection handler that owns the incoming SSH connection is ready to use it.
// The client can only be claimed once.
type PreparedSSHClient struct {
	tokenID string

	mu     sync.Mutex
	client *SSHClient
}

func NewPreparedSSHClient(token *model.ConnectToken, client *SSHClient) *PreparedSSHClient {
	tokenID := ""
	if token != nil {
		tokenID = token.Id
	}
	return &PreparedSSHClient{
		tokenID: tokenID,
		client:  client,
	}
}

func (p *PreparedSSHClient) IsValidFor(token *model.ConnectToken) bool {
	if p == nil || token == nil {
		return false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.client != nil && p.tokenID == token.Id
}

func (p *PreparedSSHClient) TakeForToken(token *model.ConnectToken) *SSHClient {
	if p == nil || token == nil {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.client == nil || p.tokenID != token.Id {
		return nil
	}
	client := p.client
	p.client = nil
	return client
}

// CloseIfUnused closes the prepared client unless its ownership has already
// been transferred with TakeForToken.
func (p *PreparedSSHClient) CloseIfUnused() {
	if p == nil {
		return
	}
	p.mu.Lock()
	client := p.client
	p.client = nil
	p.mu.Unlock()
	if client != nil {
		_ = client.Close()
	}
}

func IsSSHMFATarget(platform *model.Platform) bool {
	if platform == nil {
		return false
	}
	name := strings.ToLower(platform.Name)
	baseOS := strings.ToLower(platform.BaseOs)
	return strings.Contains(name, "mfa") || strings.Contains(baseOS, "mfa")
}
