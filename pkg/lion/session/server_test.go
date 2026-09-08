package session

import (
	"bufio"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	sshserver "github.com/gliderlabs/ssh"
	"github.com/jumpserver-dev/sdk-go/model"
	"github.com/jumpserver-dev/sdk-go/service"
	"github.com/jumpserver-dev/sdk-go/service/panda"
	gossh "golang.org/x/crypto/ssh"
)

func TestPandaAPIForwardKeepsReleaseAvailable(t *testing.T) {
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
	sshServer := &sshserver.Server{
		HostSigners: []sshserver.Signer{signer},
		MaxTimeout:  5 * time.Second,
		ChannelHandlers: map[string]sshserver.ChannelHandler{
			"direct-tcpip": func(_ *sshserver.Server, _ *gossh.ServerConn, request gossh.NewChannel, _ sshserver.Context) {
				var target struct {
					Host       string
					Port       uint32
					OriginHost string
					OriginPort uint32
				}
				if err := gossh.Unmarshal(request.ExtraData(), &target); err != nil || target.Host != "127.0.0.1" || target.Port != 9001 {
					t.Errorf("invalid Panda API destination: %+v, %v", target, err)
					_ = request.Reject(gossh.Prohibited, "invalid destination")
					return
				}
				channel, requests, err := request.Accept()
				if err != nil {
					t.Error(err)
					return
				}
				defer channel.Close()
				go gossh.DiscardRequests(requests)
				r, err := http.ReadRequest(bufio.NewReader(channel))
				if err != nil {
					t.Error(err)
					return
				}
				defer r.Body.Close()
				if r.Method != http.MethodPost || r.URL.Path != panda.ContainerReleaseURL {
					t.Errorf("unexpected Panda API request: %s %s", r.Method, r.URL.Path)
				}
				_, _ = io.Copy(io.Discard, r.Body)
				_, _ = io.WriteString(channel, "HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: 16\r\nConnection: close\r\n\r\n{\"success\":true}")
			},
		},
	}
	defer sshServer.Close()
	go func() { _ = sshServer.Serve(ln) }()
	addr := ln.Addr().(*net.TCPAddr)
	provider := &virtualAppProvider{
		Host:    model.Asset{Address: addr.IP.String(), Protocols: model.Protocols{{Name: "ssh", Port: addr.Port}}},
		Account: model.Account{BaseAccount: model.BaseAccount{Username: "test", Secret: "test"}},
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	forwarder, baseURL, err := (&Server{}).startPandaAPIForward(ctx, provider)
	if err != nil {
		t.Fatal(err)
	}
	defer forwarder.Stop()
	cancel()
	client := NewPandaClient(baseURL, model.AccessKey{ID: "key", Secret: "secret"}, false)
	if err := client.ReleaseContainer("container"); err != nil {
		t.Fatalf("Panda release through SSH after browser cancellation: %v", err)
	}
	forwarder.Stop()
	if conn, err := net.DialTimeout("tcp", forwarder.GetListenAddr().String(), time.Second); err == nil {
		_ = conn.Close()
		t.Fatal("Panda API tunnel remained open after Stop")
	}
}

func TestPandaAPIForwardRequiresSSHProvider(t *testing.T) {
	host := model.Asset{Address: "192.0.2.10", Protocols: model.Protocols{{Name: "ssh", Port: 22}}}
	for _, test := range []struct {
		name     string
		provider *virtualAppProvider
		want     string
	}{
		{"provider", nil, "provider is required"},
		{"host", &virtualAppProvider{}, "SSH host is required"},
		{"port", &virtualAppProvider{Host: model.Asset{Address: host.Address}}, "valid SSH port"},
		{"account", &virtualAppProvider{Host: host}, "SSH account and credentials"},
		{"credential", &virtualAppProvider{Host: host, Account: model.Account{BaseAccount: model.BaseAccount{Username: "root"}}}, "SSH account and credentials"},
	} {
		t.Run(test.name, func(t *testing.T) {
			forwarder, _, err := (&Server{}).startPandaAPIForward(context.Background(), test.provider)
			if err == nil || !strings.Contains(err.Error(), test.want) || forwarder != nil {
				t.Fatalf("expected %q before connecting, got %v", test.want, err)
			}
		})
	}
}

func TestVirtualAppOptionRetainsProviderAndAuthentication(t *testing.T) {
	core := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != service.SuperConnectTokenVirtualAppOptionURL ||
			r.Header.Get("Authorization") == "" || r.Header.Get("X-JMS-ORG") != "ROOT" {
			t.Errorf("invalid authenticated virtual app request: %s %s", r.Method, r.URL.Path)
		}
		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body["id"] != "token" {
			t.Errorf("invalid token body: %v", body)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"name":"browser","image_name":"browser:v1","provider":{"name":"panda","host":{"address":"192.0.2.10","protocols":[{"name":"ssh","port":2222}]},"account":{"username":"root","secret":"key"}}}`))
	}))
	defer core.Close()
	jms, err := service.NewAuthJMService(service.JMSCoreHost(core.URL), service.JMSAccessKey("key", "secret"))
	if err != nil {
		t.Fatal(err)
	}
	server := Server{JmsService: jms}
	app, err := server.getVirtualAppOption("token")
	if err != nil {
		t.Fatal(err)
	}
	if app.ImageName != "browser:v1" || app.Provider == nil {
		t.Fatalf("lost virtual app provider: %+v", app)
	}
	target, err := providerSSHTarget(app.Provider)
	if err != nil {
		t.Fatal(err)
	}
	if target.Address != "192.0.2.10" || target.Protocols.GetProtocolPort("ssh") != 2222 || target.Account.Secret != "key" {
		t.Fatalf("lost provider SSH connection fields")
	}
}
