package srvconn

import (
	"strings"
	"sync"
	"time"

	"github.com/jumpserver/koko/pkg/logger"
)

type UserSSHClient struct {
	ID      string // userID_assetID_systemUserID_systemUsername
	clients map[*sshClient]int64
	mu      sync.Mutex
}

func (u *UserSSHClient) AddClient(client *sshClient) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.clients[client] = time.Now().UnixNano()
	logger.Infof("Store new client(%s) remain %d", client, len(u.clients))
}

func (u *UserSSHClient) DeleteClient(client *sshClient) {
	u.mu.Lock()
	defer u.mu.Unlock()
	delete(u.clients, client)
	logger.Infof("Remove client(%s) remain %d", client, len(u.clients))
}

func (u *UserSSHClient) GetClient() *sshClient {
	u.mu.Lock()
	defer u.mu.Unlock()
	if len(u.clients) == 0 {
		return nil
	}

	var client *sshClient
	var latest int64
	for item, timestamp := range u.clients {
		if timestamp > latest {
			latest = timestamp
			client = item
		}
	}
	return client

}

func (u *UserSSHClient) count() int {
	u.mu.Lock()
	defer u.mu.Unlock()
	return len(u.clients)
}

type SSHManager struct {
	data map[string]*UserSSHClient
	mu   sync.Mutex
}

func (s *SSHManager) getClientFromCache(key string) (*sshClient, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if userClient, ok := s.data[key]; ok {
		client := userClient.GetClient()
		if client != nil {
			return client, true
		}
	}
	return nil, false
}

func (s *SSHManager) AddClientCache(key string, client *sshClient) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if userClient, ok := s.data[key]; ok {
		userClient.AddClient(client)
	} else {
		userClient = &UserSSHClient{
			ID:      key,
			clients: make(map[*sshClient]int64),
		}
		userClient.AddClient(client)
		s.data[key] = userClient
		logger.Infof("Add new user cache current count: %d", len(s.data))
	}
}

func (s *SSHManager) deleteClientFromCache(key string, client *sshClient) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if userClient, ok := s.data[key]; ok {
		userClient.DeleteClient(client)
		if userClient.count() == 0 {
			delete(s.data, key)
			logger.Infof("Delete user cache current count: %d", len(s.data))
		}
	}
}

func (s *SSHManager) searchSSHClientFromCache(prefixKey string) (client *sshClient, ok bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for key, userClient := range s.data {
		if strings.HasPrefix(key, prefixKey) {
			client := userClient.GetClient()
			if client != nil {
				return client, true
			}
		}
	}
	return nil, false
}
