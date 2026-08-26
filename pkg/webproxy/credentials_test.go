package webproxy

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jumpserver-dev/sdk-go/model"
)

type fakeConnectTokenService struct {
	token model.ConnectToken
	calls []bool
}

func (f *fakeConnectTokenService) GetConnectTokenInfo(_ string, expireNow bool) (model.ConnectToken, error) {
	f.calls = append(f.calls, expireNow)
	return f.token, nil
}

func TestCredentialSessionEncryptsAndReleasesOnce(t *testing.T) {
	clientKey, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	clientDER, err := x509.MarshalPKIXPublicKey(clientKey.PublicKey())
	if err != nil {
		t.Fatal(err)
	}
	service := &fakeConnectTokenService{token: testWebConnectToken()}
	proxy, err := NewServer("127.0.0.1", "0", "*", "", "", service)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(proxy)
	defer server.Close()

	createBody, _ := json.Marshal(createCredentialSessionRequest{
		TokenID: "token-id", TokenValue: "token-value",
		ClientPublicKey: base64.StdEncoding.EncodeToString(clientDER),
	})
	response, err := http.Post(server.URL+credentialPathPrefix, "application/json", bytes.NewReader(createBody))
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("unexpected create status %d: %s", response.StatusCode, body)
	}
	if bytes.Contains(body, []byte("managed-password")) || bytes.Contains(body, []byte("managed-user")) {
		t.Fatal("credential leaked in session response")
	}
	if len(service.calls) != 2 || service.calls[0] || !service.calls[1] {
		t.Fatalf("token must be validated before it is consumed: %v", service.calls)
	}
	var session createCredentialSessionResponse
	if err = json.Unmarshal(body, &session); err != nil {
		t.Fatal(err)
	}
	if !session.AutofillAvailable || session.Origin != "https://login.example.com" {
		t.Fatalf("unexpected session: %+v", session)
	}
	if session.SubmitSelector != "type=submit" {
		t.Fatalf("unexpected submit selector %q", session.SubmitSelector)
	}

	wrongOrigin := releaseCredentialRequest(t, server.URL, session, "https://evil.example.com")
	if wrongOrigin.StatusCode != http.StatusForbidden {
		t.Fatalf("unexpected wrong-origin status %d", wrongOrigin.StatusCode)
	}
	_ = wrongOrigin.Body.Close()

	released := releaseCredentialRequest(t, server.URL, session, session.Origin)
	releasedBody, _ := io.ReadAll(released.Body)
	_ = released.Body.Close()
	if released.StatusCode != http.StatusOK {
		t.Fatalf("unexpected release status %d: %s", released.StatusCode, releasedBody)
	}
	var encrypted map[string]string
	if err = json.Unmarshal(releasedBody, &encrypted); err != nil {
		t.Fatal(err)
	}
	credentials := decryptTestCredentials(t, clientKey, session, encrypted)
	if credentials.Username != "managed-user" || credentials.Password != "managed-password" {
		t.Fatal("decrypted credential does not match")
	}

	second := releaseCredentialRequest(t, server.URL, session, session.Origin)
	if second.StatusCode != http.StatusNotFound {
		t.Fatalf("credential session was reusable: %d", second.StatusCode)
	}
	_ = second.Body.Close()
}

func TestCredentialSessionRejectsWrongTokenWithoutConsumingIt(t *testing.T) {
	service := &fakeConnectTokenService{token: testWebConnectToken()}
	proxy, err := NewServer("127.0.0.1", "0", "*", "", "", service)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(proxy)
	defer server.Close()
	body, _ := json.Marshal(createCredentialSessionRequest{TokenID: "token-id", TokenValue: "wrong"})
	response, err := http.Post(server.URL+credentialPathPrefix, "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unexpected status %d", response.StatusCode)
	}
	if len(service.calls) != 1 || service.calls[0] {
		t.Fatalf("invalid token was consumed: %v", service.calls)
	}
}

func TestCredentialSelectorsAllowPasswordOnlyLogin(t *testing.T) {
	token := testWebConnectToken()
	token.Asset.SpecInfo.UsernameSelector = ""
	token.Account.Username = ""
	username, password, submit, available := credentialSelectors(token)
	if !available {
		t.Fatal("expected password-only autofill to be available")
	}
	if username != "" || password != "css=input[type=password]" || submit != "type=submit" {
		t.Fatalf("unexpected selectors %q %q %q", username, password, submit)
	}
}

func testWebConnectToken() model.ConnectToken {
	return model.ConnectToken{
		Value:    "token-value",
		Protocol: "https",
		Actions:  model.Actions{{Value: model.ActionConnect}},
		Asset: model.Asset{
			ID:      "asset-id",
			Address: "https://login.example.com/sign-in?tenant=one#ignored",
			SpecInfo: model.SpecInfo{
				Autofill: "basic", UsernameSelector: "name=username", PasswordSelector: "css=input[type=password]", SubmitSelector: "type=submit",
			},
		},
		Account: model.Account{BaseAccount: model.BaseAccount{
			ID: "account-id", Username: "managed-user", Secret: "managed-password",
			SecretType: model.LabelValue{Value: "password"},
		}},
	}
}

func releaseCredentialRequest(t *testing.T, serverURL string, session createCredentialSessionResponse, origin string) *http.Response {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"origin": origin})
	request, _ := http.NewRequest(http.MethodPost, serverURL+credentialPathPrefix+"/"+session.ID+"/credentials", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+session.AccessToken)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	return response
}

func decryptTestCredentials(t *testing.T, clientKey *ecdh.PrivateKey, session createCredentialSessionResponse, envelope map[string]string) credentialEnvelope {
	t.Helper()
	serverDER, _ := base64.StdEncoding.DecodeString(session.ServerPublicKey)
	parsed, err := x509.ParsePKIXPublicKey(serverDER)
	if err != nil {
		t.Fatal(err)
	}
	sharedSecret, err := clientKey.ECDH(parsed.(*ecdh.PublicKey))
	if err != nil {
		t.Fatal(err)
	}
	key, err := hkdf.Key(sha256.New, sharedSecret, nil, credentialKDFInfo, 32)
	if err != nil {
		t.Fatal(err)
	}
	block, _ := aes.NewCipher(key)
	gcm, _ := cipher.NewGCM(block)
	nonce, _ := base64.StdEncoding.DecodeString(envelope["nonce"])
	ciphertext, _ := base64.StdEncoding.DecodeString(envelope["ciphertext"])
	plaintext, err := gcm.Open(nil, nonce, ciphertext, []byte(session.ID+"\n"+session.Origin))
	if err != nil {
		t.Fatal(err)
	}
	var credentials credentialEnvelope
	if err = json.Unmarshal(plaintext, &credentials); err != nil {
		t.Fatal(err)
	}
	return credentials
}
