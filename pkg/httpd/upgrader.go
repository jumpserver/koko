package httpd

import (
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/kataras/neffos"

	gorilla "github.com/gorilla/websocket"
)

// DefaultUpgrader is a gorilla/websocket Upgrader with all fields set to the default values.
var DefaultUpgrader = Upgrader(gorilla.Upgrader{})

// Upgrader is a `neffos.Upgrader` type for the gorilla/websocket subprotocol implementation.
// Should be used on `New` to construct the neffos server.
func Upgrader(upgrader gorilla.Upgrader) neffos.Upgrader {
	return func(w http.ResponseWriter, r *http.Request) (neffos.Socket, error) {
		header := w.Header()
		header.Set("Access-Control-Allow-Origin", "*")
		underline, err := upgrader.Upgrade(w, r, header)
		if err != nil {
			return nil, err
		}

		return newSocket(underline, r, false), nil
	}
}

// Socket completes the `neffos.Socket` interface,
// it describes the underline websocket connection.
type Socket struct {
	UnderlyingConn *gorilla.Conn
	request        *http.Request

	client bool

	mu sync.Mutex
}

func newSocket(underline *gorilla.Conn, request *http.Request, client bool) *Socket {
	return &Socket{
		UnderlyingConn: underline,
		request:        request,
		client:         client,
	}
}

// NetConn returns the underline net connection.
func (s *Socket) NetConn() net.Conn {
	return s.UnderlyingConn.UnderlyingConn()
}

// Request returns the http request value.
func (s *Socket) Request() *http.Request {
	return s.request
}

// ReadData reads binary or text messages from the remote connection.
func (s *Socket) ReadData(timeout time.Duration) ([]byte, error) {
	for {
		if timeout > 0 {
			s.UnderlyingConn.SetReadDeadline(time.Now().Add(timeout))
		}

		opCode, data, err := s.UnderlyingConn.ReadMessage()
		if err != nil {
			return nil, err
		}

		if opCode != gorilla.BinaryMessage && opCode != gorilla.TextMessage {
			// if gorilla.IsUnexpectedCloseError(err, gorilla.CloseGoingAway) ...
			continue
		}

		return data, err
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
		s.UnderlyingConn.SetWriteDeadline(time.Now().Add(timeout))
	}

	s.mu.Lock()
	err := s.UnderlyingConn.WriteMessage(opCode, body)
	s.mu.Unlock()

	return err
}
