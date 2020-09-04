package ws

import (
	"net/http"
	"sync"
	"time"

	gorilla "github.com/gorilla/websocket"
)

type Socket struct {
	underlyingConn *gorilla.Conn
	request        *http.Request
	mu             sync.Mutex
}

func NewSocket(underWs *gorilla.Conn, request *http.Request) *Socket {
	return &Socket{
		underlyingConn: underWs,
		request:        request,
	}
}

// Request returns the http request value.
func (s *Socket) Request() *http.Request {
	return s.request
}

// ReadData reads binary or text messages from the remote connection.
func (s *Socket) ReadData(timeout time.Duration) ([]byte, int, error) {
	for {
		if timeout > 0 {
			s.underlyingConn.SetReadDeadline(time.Now().Add(timeout))
		}

		opCode, data, err := s.underlyingConn.ReadMessage()
		if err != nil {
			return nil, 0, err
		}

		if opCode != gorilla.BinaryMessage && opCode != gorilla.TextMessage {
			continue
		}

		return data, opCode, err
	}
}

// WriteBinary sends a binary message to the remote connection.
func (s *Socket) WriteBinary(body []byte, timeout time.Duration) error {
	return s.write(body, gorilla.BinaryMessage, timeout)
}

// WriteText sends a text message to the remote connection.
func (s *Socket) WriteText(body []byte, timeout time.Duration) error {
	return s.write(body, gorilla.TextMessage, timeout)
}

func (s *Socket) write(body []byte, opCode int, timeout time.Duration) error {
	if timeout > 0 {
		s.underlyingConn.SetWriteDeadline(time.Now().Add(timeout))
	}

	s.mu.Lock()
	err := s.underlyingConn.WriteMessage(opCode, body)
	s.mu.Unlock()
	return err
}

func (s *Socket) WritePing(body []byte, timeout time.Duration) error {
	return s.write(body, gorilla.PingMessage, timeout)
}

func (s *Socket) WritePong(body []byte, timeout time.Duration) error {
	return s.write(body, gorilla.PongMessage, timeout)
}

func (s *Socket) Close() error {
	return s.write(nil, gorilla.CloseMessage, -1)
}
