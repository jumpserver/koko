package webssh

import (
	"sync"

	"cocogo/pkg/model"

	socketio "github.com/googollee/go-socket.io"
)

type connections struct {
	container map[string]*WebConn
	mu        *sync.RWMutex
}

func (c *connections) GetWebConn(conID string) (conn *WebConn) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	conn = c.container[conID]
	return
}

func (c *connections) DeleteWebConn(conID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.container, conID)
}

func (c *connections) AddWebConn(conID string, conn *WebConn) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.container[conID] = conn
}

type WebConn struct {
	Cid     string
	Sock    socketio.Conn
	Addr    string
	User    *model.User
	Clients map[string]*Client
	mu      *sync.RWMutex
}

func (w *WebConn) GetClient(clientID string) (conn *Client) {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.Clients[clientID]
}

func (w *WebConn) DeleteClient(clientID string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	delete(w.Clients, clientID)
}

func (w *WebConn) AddClient(clientID string, conn *Client) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.Clients[clientID] = conn
}
