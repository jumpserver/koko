package proxy

import (
	"bytes"
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/gliderlabs/ssh"
	"github.com/jumpserver-dev/sdk-go/model"
	"github.com/jumpserver-dev/sdk-go/service"
	"github.com/jumpserver/koko/pkg/exchange"
	"github.com/jumpserver/koko/pkg/localcommand"
	"github.com/jumpserver/koko/pkg/srvconn"
)

type bridgeUSQLUser struct {
	net.Conn
	ctx context.Context
}

func (c *bridgeUSQLUser) Write(p []byte) (int, error) { return len(p), nil }
func (c *bridgeUSQLUser) ID() string                  { return "usql-cancel" }
func (c *bridgeUSQLUser) Context() context.Context    { return c.ctx }
func (c *bridgeUSQLUser) WinCh() <-chan ssh.Window    { return nil }
func (c *bridgeUSQLUser) LoginFrom() string           { return "WT" }
func (c *bridgeUSQLUser) RemoteAddr() string          { return "127.0.0.1" }
func (c *bridgeUSQLUser) HandleRoomEvent(string, *exchange.RoomMessage) {
}
func (c *bridgeUSQLUser) Pty() ssh.Pty {
	return ssh.Pty{Window: ssh.Window{Width: 80, Height: 24}}
}

type bridgeUSQLConnection struct {
	*srvconn.USQLConn
	started chan struct{}
	once    sync.Once
}

func (c *bridgeUSQLConnection) Write(p []byte) (int, error) {
	c.once.Do(func() { close(c.started) })
	return c.USQLConn.Write(p)
}

func TestBridgeCancelsBlockedUSQLInput(t *testing.T) {
	core := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("{}"))
	}))
	defer core.Close()
	api, err := service.NewAuthJMService(service.JMSCoreHost(core.URL))
	if err != nil {
		t.Fatal(err)
	}
	exchange.Initial()
	defer exchange.Close()
	command, err := localcommand.New("/bin/sh", []string{
		"-c", "stty raw -echo && printf READY && exec /bin/sleep 30",
	})
	if err != nil {
		t.Fatal(err)
	}
	defer command.Close()
	ready := make(chan error, 1)
	go func() { _, err := io.ReadFull(command, make([]byte, 5)); ready <- err }()
	select {
	case err := <-ready:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("PTY did not become ready")
	}
	reader, writer := net.Pipe()
	defer reader.Close()
	defer writer.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	user := &bridgeUSQLUser{Conn: reader, ctx: ctx}
	server := &Server{
		ID: user.ID(), UserConn: user, jmsService: api,
		connOpts: &ConnectionOptions{authInfo: &model.ConnectToken{Protocol: srvconn.ProtocolMySQL}},
		terminalConf: &model.TerminalConfig{
			ReplayStorage: model.ReplayConfig{TypeName: "null"}, CommandStorage: model.CommandConfig{TypeName: "null"},
		},
	}
	session := &SwitchSession{ID: user.ID(), p: server, ctx: context.Background(), keepAliveTime: 60}
	conn := &bridgeUSQLConnection{USQLConn: &srvconn.USQLConn{LocalCommand: command}, started: make(chan struct{})}
	finished := make(chan error, 1)
	go func() { finished <- session.Bridge(user, conn) }()
	written := make(chan error, 1)
	go func() { _, err := writer.Write(bytes.Repeat([]byte("x"), 1024*1024)); written <- err }()
	select {
	case <-conn.started:
	case <-time.After(2 * time.Second):
		t.Fatal("input did not reach USQL through the bridge and parser")
	}
	select {
	case err := <-written:
		t.Fatalf("PTY input did not block: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	cancel()
	select {
	case err := <-finished:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("cancelled bridge did not close the blocked USQL connection")
	}
	select {
	case <-written:
	case <-time.After(2 * time.Second):
		t.Fatal("bridge cancellation left user input blocked")
	}
}
