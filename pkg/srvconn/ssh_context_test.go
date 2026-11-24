package srvconn

import (
	"context"
	"errors"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jumpserver-dev/sdk-go/model"
)

func TestSSHContextBoundsUnresponsiveHandshake(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	closed := make(chan struct{})
	go func() {
		defer close(closed)
		conn, err := listener.Accept()
		if err == nil {
			defer conn.Close()
			var b [512]byte
			for {
				if _, err = conn.Read(b[:]); err != nil {
					return
				}
			}
		}
	}()
	host, port, _ := net.SplitHostPort(listener.Addr().String())
	portNumber, _ := strconv.Atoi(port)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if _, err = NewSSHClient(SSHClientHost(host), SSHClientPort(portNumber), SSHClientContext(ctx)); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("handshake did not stop at context deadline: %v", err)
	}
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("handshake transport leaked")
	}
}

func TestSSHContextRejectsInvalidKeyBeforeAuthentication(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	token := &model.ConnectToken{Account: model.Account{BaseAccount: model.BaseAccount{Secret: "not-a-key", SecretType: model.LabelValue{Value: "ssh_key"}}}}
	if _, err := NewSSHClientWithTokenContext(ctx, token, 1); err == nil || !strings.Contains(err.Error(), "invalid SSH private key") {
		t.Fatalf("invalid token key fell through to another authentication: %v", err)
	}
	if _, err := NewSSHClient(SSHClientContext(ctx), SSHClientPrivateKey("not-a-key")); err == nil || !strings.Contains(err.Error(), "invalid SSH private key") {
		t.Fatalf("invalid gateway key fell through to another authentication: %v", err)
	}
}
