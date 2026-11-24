package assetexec

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/jumpserver/koko/pkg/srvconn"
	gossh "golang.org/x/crypto/ssh"
)

func executionTestConnection(t *testing.T, complete bool) *Connection {
	t.Helper()
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := gossh.NewSignerFromKey(private)
	if err != nil {
		t.Fatal(err)
	}
	config := &gossh.ServerConfig{NoClientAuth: true}
	config.AddHostKey(signer)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		server, channels, requests, err := gossh.NewServerConn(conn, config)
		if err != nil {
			return
		}
		defer server.Close()
		go gossh.DiscardRequests(requests)
		for incoming := range channels {
			channel, requests, err := incoming.Accept()
			if err != nil {
				return
			}
			for request := range requests {
				if request.Type != "exec" {
					_ = request.Reply(false, nil)
					continue
				}
				_ = request.Reply(true, nil)
				if complete {
					_, _ = io.WriteString(channel, "out")
					_, _ = io.WriteString(channel.Stderr(), "err")
					_, _ = channel.SendRequest("exit-status", false, gossh.Marshal(struct{ Status uint32 }{7}))
					_ = channel.Close()
				}
			}
		}
	}()
	client, err := srvconn.NewSSHClient(srvconn.SSHClientHost("127.0.0.1"), srvconn.SSHClientPort(listener.Addr().(*net.TCPAddr).Port), srvconn.SSHClientTimeout(2))
	if err != nil {
		t.Fatal(err)
	}
	conn := &Connection{protocol: "ssh", ssh: client, opts: Options{MaxOutputBytes: 1024}}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

func TestSSHExecReportsRemoteExitAndSeparateStreams(t *testing.T) {
	conn := executionTestConnection(t, true)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	var stdout, stderr strings.Builder
	result, err := conn.Execute(ctx, Request{Command: "exit 7"}, func(stream string, p []byte) error {
		if stream == "stdout" {
			_, _ = stdout.Write(p)
		} else {
			_, _ = stderr.Write(p)
		}
		return nil
	})
	var exit *gossh.ExitError
	if !errors.As(err, &exit) || !result.Dispatched || !result.Completed || result.ExitCode == nil || *result.ExitCode != 7 || stdout.String() != "out" || stderr.String() != "err" {
		t.Fatalf("remote exit evidence lost: %+v %v %q %q", result, err, stdout.String(), stderr.String())
	}
}

func TestSSHCancellationDoesNotInventExitConfirmation(t *testing.T) {
	conn := executionTestConnection(t, false)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	result, err := conn.Execute(ctx, Request{Command: "sleep 60"}, func(string, []byte) error { return nil })
	if err == nil || !result.Dispatched || result.Completed || result.StopConfirmed || !result.ContextLost {
		t.Fatalf("disconnect mistaken for confirmed exit: %+v %v", result, err)
	}
}
