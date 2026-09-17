package webproxy

import (
	"bytes"
	"crypto/ecdh"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jumpserver-dev/sdk-go/model"
	"github.com/jumpserver-dev/sdk-go/service"
)

func TestWebCoreServicePreservesAuthenticationAndScript(t *testing.T) {
	var calls []bool
	core := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method != http.MethodPost || r.URL.Path != service.SuperConnectTokenSecretURL {
			t.Error("unexpected token request endpoint")
			http.Error(w, `{}`, http.StatusNotFound)
			return
		}
		if !strings.HasPrefix(r.Header.Get("Authorization"), "Signature ") || r.Header.Get("X-JMS-ORG") != "ROOT" || r.Header.Get("Date") == "" {
			t.Error("token request lost service authentication or organization context")
			http.Error(w, `{}`, http.StatusUnauthorized)
			return
		}
		var body struct {
			ID        string `json:"id"`
			ExpireNow bool   `json:"expire_now"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.ID != "token-id" {
			t.Error("invalid token request body")
			http.Error(w, `{}`, http.StatusBadRequest)
			return
		}
		calls = append(calls, body.ExpireNow)
		token := testWebConnectToken()
		_ = json.NewEncoder(w).Encode(struct {
			model.ConnectToken
			Asset any `json:"asset"`
		}{token, struct {
			model.Asset
			SpecInfo webLoginConfig `json:"spec_info"`
		}{token.Asset, webLoginConfig{Autofill: "script", Script: []webLoginStep{
			{Step: 1, Command: "type", Target: "id=password", Value: "{SECRET}"},
			{Step: 2, Command: "interactive", Target: "id=captcha"},
			{Step: 3, Command: "success", Target: "id=dashboard"},
		}}}})
	}))
	defer core.Close()
	client, err := service.NewAuthJMService(service.JMSCoreHost(core.URL), service.JMSAccessKey("test-key", "test-secret"))
	if err != nil {
		t.Fatal(err)
	}
	for _, expireNow := range []bool{false, true} {
		token, err := NewCoreService(client).GetConnectTokenInfo("token-id", expireNow)
		if err != nil {
			t.Fatal(err)
		}
		if token.Value != "token-value" || token.Asset.SpecInfo.Autofill != "script" || token.Account.Secret != "managed-password" {
			t.Fatal("token identity or account information was lost")
		}
		config, err := loginConfig(token, "https://login.example.com")
		if err != nil || config.Autofill != "script" || len(config.Script) != 3 || config.Script[1].Command != "interactive" || config.Script[2].Command != "success" {
			t.Fatal("script configuration was lost")
		}
	}
	if len(calls) != 2 || calls[0] || !calls[1] {
		t.Fatal("token must be read before it is consumed")
	}
}

func TestScriptCredentialOriginAndOneTimeRelease(t *testing.T) {
	token := testWebConnectToken()
	token.Asset.SpecInfo = model.SpecInfo{Autofill: "script"}
	service := &fakeConnectTokenService{token: token, config: webLoginConfig{
		Autofill: "script",
		Script: []webLoginStep{
			{Step: 1, Command: "type", Target: "id=password", Value: "{SECRET}", Origin: "https://sso.example.com"},
			{Step: 2, Command: "success", Target: "id=dashboard"},
		},
	}}
	proxy, err := NewServer("127.0.0.1", "0", "", "", service)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(proxy)
	defer server.Close()
	defer proxy.stopProxySessions()
	key, _ := ecdh.X25519().GenerateKey(rand.Reader)
	publicKey, _ := x509.MarshalPKIXPublicKey(key.PublicKey())
	request, _ := json.Marshal(createCredentialSessionRequest{TokenID: "token-id", TokenValue: "token-value", ClientPublicKey: base64.StdEncoding.EncodeToString(publicKey)})
	httpRequest, _ := http.NewRequest(http.MethodPost, server.URL+credentialPathPrefix, bytes.NewReader(request))
	httpRequest.Header.Set("X-Koko-Connect-Ticket", testConnectTicket(token))
	response, err := http.DefaultClient.Do(httpRequest)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var session createCredentialSessionResponse
	if err = json.NewDecoder(response.Body).Decode(&session); err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusCreated || !session.AutofillAvailable || session.Autofill != "script" || len(session.Script) != 2 || session.PasswordSelector != "" {
		t.Fatal("script session was not returned independently of basic selectors")
	}
	if session.Script[1].Origin != session.Origin {
		t.Fatal("missing step origin must default to the asset")
	}
	for _, origin := range []string{session.Origin, "https://evil.example.com"} {
		denied := releaseCredentialRequest(t, server.URL, session, origin)
		denied.Body.Close()
		if denied.StatusCode != http.StatusForbidden {
			t.Fatal("navigation permission must not grant credentials")
		}
	}
	released := releaseCredentialRequest(t, server.URL, session, "https://sso.example.com")
	defer released.Body.Close()
	var encrypted map[string]string
	if err = json.NewDecoder(released.Body).Decode(&encrypted); err != nil {
		t.Fatal(err)
	}
	if released.StatusCode != http.StatusOK || decryptTestCredentials(t, key, session, encrypted).Password != "managed-password" {
		t.Fatal("SSO credentials were not released")
	}
	again := releaseCredentialRequest(t, server.URL, session, "https://sso.example.com")
	again.Body.Close()
	if again.StatusCode != http.StatusNotFound {
		t.Fatal("SSO credentials were released twice")
	}
}

func TestScriptValidatesOriginsWithoutRestrictingSSONavigation(t *testing.T) {
	for _, raw := range []string{"https://*.example.com", "https://user@example.com", "https://example.com/path", "https://example.com#", "https://example.com:99999"} {
		if _, err := exactOrigin(raw); err == nil {
			t.Fatalf("accepted invalid origin %q", raw)
		}
	}
	for _, step := range []webLoginStep{
		{Step: 1, Command: "open", Value: "file:///tmp"},
		{Step: 1, Command: "open", Value: "javascript:alert(1)"},
		{Step: 1, Command: "open", Value: "https://sso.example.com/{SECRET}"},
		{Step: 1, Command: "type", Target: "id=password", Value: "{SECRET}", Origin: "https://sso.example.com/login"},
		{Step: 1, Command: "select_frame", Target: "id=frame"},
	} {
		token := webConnectToken{ConnectToken: testWebConnectToken(), config: webLoginConfig{Autofill: "script", Script: []webLoginStep{step}}}
		if _, err := loginConfig(token, "https://login.example.com"); err == nil {
			t.Fatalf("accepted unsafe command %s", step.Command)
		}
	}
	token := webConnectToken{ConnectToken: testWebConnectToken(), config: webLoginConfig{Autofill: "script", Script: []webLoginStep{
		{Step: 1, Command: "open", Value: "https://sso.example.com/login"},
		{Step: 2, Command: "check", Target: "id=next", Origin: "https://sso.example.com"},
	}}}
	config, err := loginConfig(token, "https://login.example.com")
	if err != nil {
		t.Fatal(err)
	}
	if len(scriptCredentialOrigins(config, "https://login.example.com")) != 0 {
		t.Fatal("navigation-only scripts must not release credentials")
	}
}
