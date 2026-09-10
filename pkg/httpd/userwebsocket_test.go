package httpd

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	gorilla "github.com/gorilla/websocket"
	"github.com/jumpserver/koko/pkg/httpd/ws"
)

func TestWebsocketPingPong(t *testing.T) {
	for _, protocol := range []string{"json", "envelope"} {
		t.Run(protocol, func(t *testing.T) {
			envelopeProtocol := protocol == "envelope"
			messages := make(chan *Message, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				conn, err := upGrader.Upgrade(w, r, nil)
				if err != nil {
					t.Error(err)
					return
				}
				defer conn.Close()
				userCon := &UserWebsocket{
					conn: ws.NewSocket(conn, r), messageChannel: messages,
					envelopeProtocol: envelopeProtocol,
				}
				_ = userCon.readMessageLoop()
			}))
			defer server.Close()
			conn, _, err := gorilla.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			ping := &Message{Type: PING, Data: "heartbeat"}
			payload, err := json.Marshal(ping)
			opcode := gorilla.TextMessage
			if envelopeProtocol {
				payload, err = encodeMessageEnvelope(ping)
				opcode = gorilla.BinaryMessage
			}
			if err != nil {
				t.Fatal(err)
			}
			if err = conn.WriteMessage(opcode, payload); err != nil {
				t.Fatal(err)
			}
			select {
			case msg := <-messages:
				if msg.Type != PONG || msg.Data != ping.Data {
					t.Fatalf("unexpected heartbeat response: %+v", msg)
				}
			case <-time.After(time.Second):
				t.Fatal("no heartbeat response")
			}
		})
	}
}

func TestWebsocketCloseReason(t *testing.T) {
	for _, tc := range []struct {
		operation string
		err       error
		want      string
	}{
		{"read", os.ErrDeadlineExceeded, "read_timeout"},
		{"write", os.ErrDeadlineExceeded, "write_timeout"},
		{"read", &gorilla.CloseError{Code: 1000}, ""},
		{"read", &gorilla.CloseError{Code: 1006}, ""},
		{"read", io.EOF, ""},
		{"write", errors.New("broken pipe"), "write_failed"},
	} {
		if got := websocketErrorReason(tc.operation, tc.err); got != tc.want {
			t.Errorf("%s %v: got %q, want %q", tc.operation, tc.err, got, tc.want)
		}
	}
}

func TestWebsocketDoesNotQueueAfterClose(t *testing.T) {
	done := make(chan struct{})
	close(done)
	userCon := &UserWebsocket{
		done: done, messageChannel: make(chan *Message, 1),
		conn: ws.NewSocket(nil, httptest.NewRequest(http.MethodGet, "/", nil)),
	}
	userCon.SendMessage(&Message{Type: CLOSE})
	if len(userCon.messageChannel) != 0 {
		t.Fatal("queued a message after the writer stopped")
	}
}

func TestWebsocketSendsKokoCloseFrame(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upGrader.Upgrade(w, r, nil)
		if err != nil {
			t.Error(err)
			return
		}
		// Missing handler is an initialization failure, before any API calls.
		userCon := &UserWebsocket{conn: ws.NewSocket(conn, r)}
		userCon.Run()
	}))
	defer server.Close()
	conn, _, err := gorilla.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetReadDeadline(time.Now().Add(time.Second))
	_, _, err = conn.ReadMessage()
	var closeErr *gorilla.CloseError
	if !errors.As(err, &closeErr) || closeErr.Code != 4000 || closeErr.Text != "koko:initialization_failed" {
		t.Fatalf("unexpected close frame: %v", err)
	}
}
