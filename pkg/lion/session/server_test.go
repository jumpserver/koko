package session

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jumpserver-dev/sdk-go/model"
	"github.com/jumpserver-dev/sdk-go/service"
)

func TestPandaAPIForwardAddress(t *testing.T) {
	for _, test := range []struct {
		serviceURL string
		want       string
	}{
		{"http://provider.example:9001", net.JoinHostPort("127.0.0.1", "9001")},
		{"https://provider.example", net.JoinHostPort("127.0.0.1", "443")},
	} {
		got, _, err := pandaAPIForwardAddress(test.serviceURL)
		if err != nil {
			t.Fatalf("pandaAPIForwardAddress(%q): %v", test.serviceURL, err)
		}
		if got != test.want {
			t.Fatalf("pandaAPIForwardAddress(%q) = %q, want %q", test.serviceURL, got, test.want)
		}
	}
	for _, invalid := range []string{"", "provider:9001", "ftp://provider", "http://user:password@provider"} {
		if _, _, err := pandaAPIForwardAddress(invalid); err == nil {
			t.Errorf("accepted invalid Panda URL %q", invalid)
		}
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
		_, _ = w.Write([]byte(`{"name":"browser","image_name":"browser:v1","provider":{"name":"panda","service_url":"http://127.0.0.1:9001","host":{"address":"192.0.2.10","protocols":[{"name":"ssh","port":2222}]},"account":{"username":"root","secret":"key"}}}`))
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
