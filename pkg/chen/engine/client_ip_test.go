package engine

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jumpserver/koko/pkg/chen/core"
)

type addressProvider struct {
	core.Provider
	remote chan string
}

func (p *addressProvider) Open(_ context.Context, _, remote string) (*core.Grant, error) {
	p.remote <- remote
	return nil, core.ErrControlNotReady
}

func TestSessionClientAddress(t *testing.T) {
	const peer = "[fc00:1010:1111:200::f]:43210"
	cases := []struct {
		name, forwarded, want string
	}{
		{name: "direct", want: peer},
		{name: "forwarded IPv4", forwarded: "203.0.113.7", want: "203.0.113.7"},
		{name: "proxy chain", forwarded: " 203.0.113.7 , 127.0.0.1", want: "203.0.113.7"},
		{name: "forwarded IPv6", forwarded: "2001:db8::7, ::1", want: "2001:db8::7"},
		{name: "invalid first entry", forwarded: "invalid, 203.0.113.7", want: peer},
		{name: "empty first entry", forwarded: ", 203.0.113.7", want: peer},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			provider := &addressProvider{remote: make(chan string, 1)}
			server := New(provider, Options{StateDir: t.TempDir()})
			t.Cleanup(server.Close)
			body := `{"connect_token":"test-token","request_id":"client-ip-regression"}`
			request := httptest.NewRequest(http.MethodPost, "/koko/chen/api/v1/sessions", strings.NewReader(body))
			request.RemoteAddr = peer
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("X-Forwarded-For", tc.forwarded)
			response := httptest.NewRecorder()
			server.Handler().ServeHTTP(response, request)
			select {
			case got := <-provider.remote:
				if got != tc.want {
					t.Fatalf("provider received address %q; want %q", got, tc.want)
				}
			default:
				t.Fatalf("provider was not called: %d %s", response.Code, response.Body.String())
			}
		})
	}
}
