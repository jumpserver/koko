package gateway

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"io"
	"net"
	"testing"
	"time"

	sshserver "github.com/gliderlabs/ssh"
	"github.com/jumpserver-dev/sdk-go/model"
	gossh "golang.org/x/crypto/ssh"
)

func testSSHGateway(t *testing.T, handler sshserver.ChannelHandler) *model.Gateway {
	t.Helper()
	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := gossh.NewSignerFromKey(key)
	if err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := &sshserver.Server{
		HostSigners:                 []sshserver.Signer{signer},
		MaxTimeout:                  5 * time.Second,
		ChannelHandlers:             map[string]sshserver.ChannelHandler{"direct-tcpip": handler},
		LocalPortForwardingCallback: func(sshserver.Context, string, uint32) bool { return true },
	}
	t.Cleanup(func() { _ = srv.Close() })
	go func() { _ = srv.Serve(ln) }()
	addr := ln.Addr().(*net.TCPAddr)
	return &model.Gateway{
		Name: "test", Address: addr.IP.String(),
		Protocols: model.Protocols{{Name: "ssh", Port: addr.Port}},
		Account:   model.Account{BaseAccount: model.BaseAccount{Username: "test"}},
	}
}

func TestDomainGatewayRoundTripAndCancel(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
		_, _ = io.Copy(conn, conn)
	}()
	forwarder := DomainGateway{
		DstAddr:         ln.Addr().String(),
		SelectedGateway: testSSHGateway(t, sshserver.DirectTCPIPHandler),
		Destination:     testSSHGateway(t, sshserver.DirectTCPIPHandler),
	}
	defer forwarder.Stop()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := forwarder.StartContext(ctx); err != nil {
		t.Fatal(err)
	}
	conn, err := net.DialTimeout("tcp", forwarder.GetListenAddr().String(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
	const payload = "virtual-app SSH forwarding"
	if _, err := io.WriteString(conn, payload); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, len(payload))
	if _, err := io.ReadFull(conn, buf); err != nil {
		t.Fatal(err)
	}
	if string(buf) != payload {
		t.Fatalf("forwarded bytes = %q", buf)
	}
	cancel()
	if _, err := conn.Read(buf); err == nil {
		t.Fatal("connection remained open after cancellation")
	} else if timeout, ok := err.(net.Error); ok && timeout.Timeout() {
		t.Fatalf("cancellation did not close the connection: %v", err)
	}
}

func TestDomainGatewayHandshakeContext(t *testing.T) {
	for _, viaJump := range []bool{false, true} {
		for _, cancelEarly := range []bool{false, true} {
			name := "direct"
			if viaJump {
				name = "jump"
			}
			if cancelEarly {
				name += "/cancel"
			} else {
				name += "/timeout"
			}
			t.Run(name, func(t *testing.T) {
				ln, err := net.Listen("tcp", "127.0.0.1:0")
				if err != nil {
					t.Fatal(err)
				}
				defer ln.Close()
				accepted := make(chan struct{})
				closed := make(chan struct{})
				go func() {
					defer close(closed)
					conn, err := ln.Accept()
					if err != nil {
						return
					}
					defer conn.Close()
					_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
					close(accepted)
					_, _ = io.Copy(io.Discard, conn) // No SSH banner: handshake must be interrupted.
				}()
				addr := ln.Addr().(*net.TCPAddr)
				forwarder := DomainGateway{Destination: &model.Gateway{
					Address: addr.IP.String(), Protocols: model.Protocols{{Name: "ssh", Port: addr.Port}},
				}}
				defer forwarder.Stop()
				if viaJump {
					forwarder.SelectedGateway = testSSHGateway(t, sshserver.DirectTCPIPHandler)
				}
				ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
				defer cancel()
				expected := context.DeadlineExceeded
				if cancelEarly {
					expected = context.Canceled
					go func() {
						select {
						case <-accepted:
							cancel()
						case <-ctx.Done():
						}
					}()
				}
				if err := forwarder.StartContext(ctx); !errors.Is(err, expected) {
					t.Fatalf("StartContext error = %v, want %v", err, expected)
				}
				select {
				case <-closed:
				case <-time.After(time.Second):
					t.Fatal("handshake connection was not closed")
				}
			})
		}
	}
}

func TestDomainGatewayChannelOpenTimeout(t *testing.T) {
	closed := make(chan struct{})
	jump := testSSHGateway(t, func(_ *sshserver.Server, _ *gossh.ServerConn, _ gossh.NewChannel, ctx sshserver.Context) {
		<-ctx.Done() // No channel-open reply.
		close(closed)
	})
	forwarder := DomainGateway{SelectedGateway: jump, Destination: jump}
	defer forwarder.Stop()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if err := forwarder.StartContext(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("StartContext error = %v", err)
	}
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("jump connection was not closed")
	}
}
