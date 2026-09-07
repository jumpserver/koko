package tunnel

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"io"
	"net"
	"testing"
	"time"

	sshserver "github.com/gliderlabs/ssh"
	"github.com/jumpserver-dev/sdk-go/model"
	"github.com/jumpserver/koko/pkg/lion/guacd"
	"github.com/jumpserver/koko/pkg/lion/session"
	gossh "golang.org/x/crypto/ssh"
)

func TestForwardVirtualAppDesktopAndSFTP(t *testing.T) {
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
	ports := make(chan uint32, 2)
	server := &sshserver.Server{
		HostSigners: []sshserver.Signer{signer}, MaxTimeout: 5 * time.Second,
		ChannelHandlers: map[string]sshserver.ChannelHandler{
			"direct-tcpip": func(_ *sshserver.Server, _ *gossh.ServerConn, request gossh.NewChannel, _ sshserver.Context) {
				var target struct {
					Host       string
					Port       uint32
					Origin     string
					OriginPort uint32
				}
				if err := gossh.Unmarshal(request.ExtraData(), &target); err != nil {
					t.Error(err)
					_ = request.Reject(gossh.ConnectionFailed, "invalid target")
					return
				}
				ports <- target.Port
				channel, requests, err := request.Accept()
				if err != nil {
					return
				}
				defer channel.Close()
				go gossh.DiscardRequests(requests)
				_, _ = io.Copy(channel, channel)
			},
		},
	}
	defer server.Close()
	go func() { _ = server.Serve(ln) }()
	addr := ln.Addr().(*net.TCPAddr)
	sess := &session.TunnelSession{
		GatewayTarget: &model.Gateway{
			Address: addr.IP.String(), Protocols: model.Protocols{{Name: "ssh", Port: addr.Port}},
			Account: model.Account{BaseAccount: model.BaseAccount{Username: "panda"}},
		},
		VirtualAppOpts: &model.VirtualAppContainer{Host: "127.0.0.1", Port: 6900, SFTPPort: 6901},
		ActionPerm:     &session.ActionPermission{EnableUpload: true},
	}
	conf := sess.GuaConfiguration()
	stop, err := forwardSession(context.Background(), sess, &conf)
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	for _, parameters := range [][2]string{{guacd.Hostname, guacd.Port}, {guacd.SftpHostname, guacd.SftpPort}} {
		conn, err := net.DialTimeout("tcp", net.JoinHostPort(conf.GetParameter(parameters[0]), conf.GetParameter(parameters[1])), time.Second)
		if err != nil {
			t.Fatal(err)
		}
		_ = conn.SetDeadline(time.Now().Add(time.Second))
		_, err = io.WriteString(conn, parameters[0])
		buf := make([]byte, len(parameters[0]))
		if err == nil {
			_, err = io.ReadFull(conn, buf)
		}
		_ = conn.Close()
		if err != nil || string(buf) != parameters[0] {
			t.Fatalf("%s forwarding failed: %v", parameters[0], err)
		}
	}
	if first, second := <-ports, <-ports; first != 6900 || second != 6901 {
		t.Fatalf("forwarded ports = %d, %d", first, second)
	}
}
