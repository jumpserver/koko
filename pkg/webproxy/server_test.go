package webproxy

import (
	"bufio"
	"bytes"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"github.com/jumpserver-dev/sdk-go/model"
	"github.com/jumpserver/koko/pkg/auth"
	"github.com/jumpserver/koko/pkg/session"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

func authenticatedTestProxy(t *testing.T) (*Server, createCredentialSessionResponse) {
	t.Helper()
	token := testWebConnectToken()
	token.Asset.SpecInfo.Autofill = "none"
	proxy, err := NewServer("127.0.0.1", "0", "", "", &fakeConnectTokenService{token: token})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(proxy.stopProxySessions)
	request := httptest.NewRequest(http.MethodPost, credentialPathPrefix, bytes.NewBufferString(`{"token_id":"token-id","token_value":"token-value"}`))
	request.RemoteAddr = "192.0.2.1:1000"
	request.Header.Set("X-Koko-Connect-Ticket", testConnectTicket(token))
	response := httptest.NewRecorder()
	proxy.ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("create session: %d %s", response.Code, response.Body.String())
	}
	var session createCredentialSessionResponse
	if err := json.Unmarshal(response.Body.Bytes(), &session); err != nil {
		t.Fatal(err)
	}
	if session.ProxyAuth != "connect_ticket" || session.AutofillAvailable {
		t.Fatal("authenticated proxy must work without autofill")
	}
	return proxy, session
}

func proxyHeader(proxy *Server, id string) string {
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(id+":"+proxy.auth.sessions[id].ticket))
}

func TestProxyRequiresAuthenticationForHTTPAndCONNECT(t *testing.T) {
	proxy, session := authenticatedTestProxy(t)
	for _, method := range []string{http.MethodGet, http.MethodConnect} {
		for _, header := range []string{"", "Basic malformed", "Bearer token-value", "Basic " + base64.StdEncoding.EncodeToString([]byte("token-id:wrong"))} {
			request := httptest.NewRequest(method, "http://unresolvable.invalid:443", nil)
			request.Header.Set("Proxy-Authorization", header)
			response := httptest.NewRecorder()
			proxy.ServeHTTP(response, request)
			if response.Code != http.StatusProxyAuthRequired || response.Header().Get("Proxy-Authenticate") == "" {
				t.Fatalf("%s: expected authentication challenge, got %d", method, response.Code)
			}
		}
	}
	// An expired session cannot be reused even with the correct credentials.
	proxy.auth.sessions[session.SessionID].lastSeen = time.Now().Add(-proxySessionIdleTimeout)
	request := httptest.NewRequest(http.MethodGet, "http://unresolvable.invalid", nil)
	request.Header.Set("Proxy-Authorization", proxyHeader(proxy, session.SessionID))
	response := httptest.NewRecorder()
	proxy.ServeHTTP(response, request)
	if response.Code != http.StatusProxyAuthRequired {
		t.Fatalf("expired session: %d", response.Code)
	}
}

func TestAuthenticatedProxyForwardsHTTPAndHTTPSWithoutHostAllowlist(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Proxy-Authorization") != "" {
			t.Error("proxy credential reached upstream")
		}
		if r.Header.Get("Authorization") != "Bearer website" {
			t.Error("website authentication was lost")
		}
		_, _ = io.WriteString(w, "proxied")
	})
	upstream := httptest.NewServer(handler)
	defer upstream.Close()
	secureUpstream := httptest.NewTLSServer(handler)
	defer secureUpstream.Close()
	proxy, session := authenticatedTestProxy(t)
	server := httptest.NewServer(proxy)
	defer server.Close()
	proxyURL, _ := url.Parse(server.URL)
	proxyURL.User = url.UserPassword(session.SessionID, proxy.auth.sessions[session.SessionID].ticket)
	transport := &http.Transport{Proxy: http.ProxyURL(proxyURL), TLSClientConfig: &tls.Config{RootCAs: secureUpstream.Client().Transport.(*http.Transport).TLSClientConfig.RootCAs}}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 5 * time.Second}
	for _, target := range []string{upstream.URL, secureUpstream.URL} {
		request, _ := http.NewRequest(http.MethodGet, target, nil)
		request.Header.Set("Authorization", "Bearer website")
		response, err := client.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(response.Body)
		response.Body.Close()
		if err != nil || response.StatusCode != 200 || string(body) != "proxied" {
			t.Fatalf("unexpected proxy response: %d %q %v", response.StatusCode, body, err)
		}
	}
}

func TestClosingProxySessionClosesExistingTunnel(t *testing.T) {
	upstream, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer upstream.Close()
	go func() {
		conn, err := upstream.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		_, _ = io.Copy(conn, conn)
	}()
	proxy, session := authenticatedTestProxy(t)
	server := httptest.NewServer(proxy)
	defer server.Close()
	address, _ := url.Parse(server.URL)
	conn, err := net.DialTimeout("tcp", address.Host, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(5 * time.Second))
	request, _ := http.NewRequest(http.MethodConnect, "http://"+upstream.Addr().String(), nil)
	request.Header.Set("Proxy-Authorization", proxyHeader(proxy, session.SessionID))
	if err = request.Write(conn); err != nil {
		t.Fatal(err)
	}
	reader := bufio.NewReader(conn)
	response, err := http.ReadResponse(reader, request)
	if err != nil || response.StatusCode != 200 {
		t.Fatalf("CONNECT failed: %v", err)
	}
	if _, err = conn.Write([]byte("hello")); err != nil {
		t.Fatal(err)
	}
	echo := make([]byte, 5)
	if _, err = io.ReadFull(reader, echo); err != nil || string(echo) != "hello" {
		t.Fatalf("tunnel data: %q %v", echo, err)
	}
	header := proxyHeader(proxy, session.SessionID)
	closeRequest := httptest.NewRequest(http.MethodDelete, credentialPathPrefix+"/"+session.SessionID, nil)
	closeRequest.Header.Set("Proxy-Authorization", header)
	closed := httptest.NewRecorder()
	proxy.ServeHTTP(closed, closeRequest)
	if closed.Code != http.StatusNoContent {
		t.Fatalf("close: %d %s", closed.Code, closed.Body.String())
	}
	if _, err = reader.ReadByte(); err != io.EOF {
		t.Fatalf("closed session left tunnel open: %v", err)
	}
	denied := httptest.NewRecorder()
	proxy.ServeHTTP(denied, request)
	if denied.Code != http.StatusProxyAuthRequired {
		t.Fatalf("revoked session: %d", denied.Code)
	}
}

func TestProxySessionUsesSharedSessionTasksAndOutlivesBootstrapTicket(t *testing.T) {
	proxy, created := authenticatedTestProxy(t)
	header := proxyHeader(proxy, created.SessionID)
	ticketID := proxy.auth.sessions[created.SessionID].ticket
	auth.ConnectTickets.Delete(ticketID)
	// Like an established WebSocket, deleting the bootstrap ticket does not end the session.
	request := httptest.NewRequest(http.MethodPost, credentialPathPrefix+"/"+created.SessionID+"/heartbeat", nil)
	request.Header.Set("Proxy-Authorization", header)
	response := httptest.NewRecorder()
	proxy.ServeHTTP(response, request)
	if response.Code != 204 {
		t.Fatalf("established session lost authentication: %d", response.Code)
	}
	tracked, ok := session.GetSessionById(created.SessionID)
	if !ok || tracked.TokenId != "token-id" {
		t.Fatal("Web proxy was not registered with the common session manager")
	}
	for _, task := range []string{model.TaskLockSession, model.TaskUnlockSession, model.TaskPermExpired} {
		if err := tracked.HandleTask(&model.TerminalTask{Name: task}); err != nil {
			t.Fatal(err)
		}
		current := proxy.authenticateProxy(request)
		if task == model.TaskPermExpired {
			if current != nil {
				t.Fatal("expired permission still authenticated")
			}
		} else if current == nil || current.locked != (task == model.TaskLockSession) {
			t.Fatal("session lock state was not applied")
		}
	}
	if _, ok := session.GetSessionById(created.SessionID); ok {
		t.Fatal("closed proxy remained in the session registry")
	}
}
