package session

import (
	"net"
	"testing"

	"github.com/jumpserver-dev/sdk-go/model"
	"github.com/jumpserver-dev/sdk-go/service/panda"
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
}

func TestPandaClientForProvider(t *testing.T) {
	defaultClient := &panda.Client{BaseURL: "http://default-panda:9001"}
	server := Server{
		PandaClient: defaultClient,
		PandaClientFactory: func(serviceURL string) *panda.Client {
			return &panda.Client{BaseURL: serviceURL}
		},
	}

	if got := server.pandaClientFor(nil); got != defaultClient {
		t.Fatal("legacy virtual app must use the default Panda client")
	}
	provider := &model.VirtualAppProvider{ServiceURL: "https://remote-panda.example"}
	if got := server.pandaClientFor(provider); got.BaseURL != provider.ServiceURL {
		t.Fatalf("provider Panda URL = %q, want %q", got.BaseURL, provider.ServiceURL)
	}
}
