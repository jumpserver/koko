package httpd

import (
	"sync"

	"github.com/jumpserver/koko/pkg/logger"
)

var conns = &Connections{container: make(map[string][]string), mu: new(sync.RWMutex)}
var clients = &Clients{container: make(map[string]*Client), mu: new(sync.RWMutex)}

type Clients struct {
	container map[string]*Client
	mu        *sync.RWMutex
}

func (c *Clients) GetClient(cID string) (client *Client) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	client = c.container[cID]
	return
}

func (c *Clients) DeleteClient(cID string) {
	c.mu.RLock()
	client, ok := c.container[cID]
	c.mu.RUnlock()
	if !ok {
		return
	}
	_ = client.Close()
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.container, cID)
	logger.Debug("Remain clients count: ", len(c.container))
}

func (c *Clients) AddClient(cID string, conn *Client) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.container[cID] = conn
	logger.Debug("Now clients count: ", len(c.container))
}

type Connections struct {
	container map[string][]string
	mu        *sync.RWMutex
}

func (c *Connections) AddClient(cID, clientID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	clients, ok := c.container[cID]
	if ok {
		clients = append(clients, clientID)
	} else {
		clients = []string{clientID}
	}
	c.container[cID] = clients
}

func (c *Connections) GetClients(cID string) (clients []string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.container[cID]
}

func (c *Connections) DeleteClients(cID string) {
	if clientIDs := c.GetClients(cID); clientIDs != nil {
		for _, clientID := range clientIDs {
			clients.DeleteClient(clientID)
		}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.container, cID)
}
