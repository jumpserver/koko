package webproxy

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jumpserver-dev/sdk-go/model"
	"github.com/jumpserver/koko/pkg/logger"
)

const (
	credentialPathPrefix  = "/_jumpserver/web-sessions"
	credentialSessionTTL  = 5 * time.Minute
	maxCredentialSessions = 128
	credentialKDFInfo     = "jumpserver-web-autofill-v1"
)

type connectTokenService interface {
	GetConnectTokenInfo(tokenID string, expireNow bool) (model.ConnectToken, error)
}

type credentialManager struct {
	mu           sync.Mutex
	tokenService connectTokenService
	sessions     map[string]*credentialSession
}

type credentialSession struct {
	id          string
	accessToken string
	origin      string
	nonce       string
	ciphertext  string
	assetID     string
	accountID   string
	expiresAt   time.Time
}

type createCredentialSessionRequest struct {
	TokenID         string `json:"token_id"`
	TokenValue      string `json:"token_value"`
	ClientPublicKey string `json:"client_public_key"`
}

type createCredentialSessionResponse struct {
	ID                string    `json:"id,omitempty"`
	AccessToken       string    `json:"access_token,omitempty"`
	TargetURL         string    `json:"target_url"`
	Origin            string    `json:"origin"`
	AutofillAvailable bool      `json:"autofill_available"`
	UsernameSelector  string    `json:"username_selector,omitempty"`
	PasswordSelector  string    `json:"password_selector,omitempty"`
	SubmitSelector    string    `json:"submit_selector,omitempty"`
	ServerPublicKey   string    `json:"server_public_key,omitempty"`
	ExpiresAt         time.Time `json:"expires_at,omitempty"`
}

type credentialEnvelope struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func newCredentialManager(tokenService connectTokenService) *credentialManager {
	return &credentialManager{tokenService: tokenService, sessions: make(map[string]*credentialSession)}
}

func (s *Server) serveCredentials(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if !isLoopbackRemote(r.RemoteAddr) {
		http.Error(w, "credential control API is loopback-only", http.StatusForbidden)
		return
	}
	if s.credentials == nil || s.credentials.tokenService == nil {
		http.Error(w, "Web credential service is disabled", http.StatusNotFound)
		return
	}

	path := strings.Trim(strings.TrimPrefix(r.URL.Path, credentialPathPrefix), "/")
	parts := strings.Split(path, "/")
	switch {
	case path == "" && r.Method == http.MethodPost:
		s.createCredentialSession(w, r)
	case len(parts) == 2 && parts[1] == "credentials" && r.Method == http.MethodPost:
		s.releaseCredentials(w, r, parts[0])
	default:
		http.Error(w, "credential endpoint not found", http.StatusNotFound)
	}
}

func (s *Server) createCredentialSession(w http.ResponseWriter, r *http.Request) {
	var request createCredentialSessionRequest
	if err := decodeLimitedJSON(w, r, &request); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if len(request.TokenID) < 1 || len(request.TokenID) > 256 || len(request.TokenValue) < 1 || len(request.TokenValue) > 512 {
		http.Error(w, "invalid connection token", http.StatusBadRequest)
		return
	}

	connectToken, err := s.credentials.tokenService.GetConnectTokenInfo(request.TokenID, false)
	if err != nil || !secureEqual(connectToken.Value, request.TokenValue) {
		http.Error(w, "invalid connection token", http.StatusUnauthorized)
		return
	}
	connectToken, err = s.credentials.tokenService.GetConnectTokenInfo(request.TokenID, true)
	if err != nil || !secureEqual(connectToken.Value, request.TokenValue) {
		http.Error(w, "invalid connection token", http.StatusUnauthorized)
		return
	}
	if !connectToken.Actions.EnableConnect() {
		http.Error(w, "connection permission denied", http.StatusForbidden)
		return
	}

	targetURL, origin, err := credentialTarget(connectToken)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	response := createCredentialSessionResponse{TargetURL: targetURL, Origin: origin}
	usernameSelector, passwordSelector, submitSelector, available := credentialSelectors(connectToken)
	if !available {
		writeCredentialJSON(w, http.StatusCreated, response)
		return
	}

	sessionID, err := randomCredentialValue(16)
	if err != nil {
		http.Error(w, "unable to create credential session", http.StatusInternalServerError)
		return
	}
	accessToken, err := randomCredentialValue(32)
	if err != nil {
		http.Error(w, "unable to create credential session", http.StatusInternalServerError)
		return
	}
	serverPublicKey, nonce, ciphertext, err := sealCredentials(
		request.ClientPublicKey,
		sessionID,
		origin,
		credentialEnvelope{Username: connectToken.Account.Username, Password: connectToken.Account.Secret},
	)
	connectToken.Account.Secret = ""
	if err != nil {
		http.Error(w, "invalid client encryption key", http.StatusBadRequest)
		return
	}

	expiresAt := time.Now().UTC().Add(credentialSessionTTL)
	session := &credentialSession{
		id: sessionID, accessToken: accessToken, origin: origin,
		nonce: nonce, ciphertext: ciphertext,
		assetID: connectToken.Asset.ID, accountID: connectToken.Account.ID, expiresAt: expiresAt,
	}
	if err = s.credentials.save(session); err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	response.ID = sessionID
	response.AccessToken = accessToken
	response.AutofillAvailable = true
	response.UsernameSelector = usernameSelector
	response.PasswordSelector = passwordSelector
	response.SubmitSelector = submitSelector
	response.ServerPublicKey = serverPublicKey
	response.ExpiresAt = expiresAt
	writeCredentialJSON(w, http.StatusCreated, response)
}

func (s *Server) releaseCredentials(w http.ResponseWriter, r *http.Request, sessionID string) {
	var request struct {
		Origin string `json:"origin"`
	}
	if err := decodeLimitedJSON(w, r, &request); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	authorization := strings.TrimSpace(r.Header.Get("Authorization"))
	if !strings.HasPrefix(authorization, "Bearer ") {
		http.Error(w, "credential session unauthorized", http.StatusUnauthorized)
		return
	}
	session, status := s.credentials.take(sessionID, strings.TrimPrefix(authorization, "Bearer "), request.Origin)
	if status != http.StatusOK {
		http.Error(w, http.StatusText(status), status)
		return
	}
	logger.Infof("Web credentials released for session %s asset %s account %s", session.id, session.assetID, session.accountID)
	writeCredentialJSON(w, http.StatusOK, map[string]string{"nonce": session.nonce, "ciphertext": session.ciphertext})
}

func (m *credentialManager) save(session *credentialSession) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.removeExpired(time.Now())
	if len(m.sessions) >= maxCredentialSessions {
		return errors.New("too many Web credential sessions")
	}
	m.sessions[session.id] = session
	return nil
}

func (m *credentialManager) take(id, accessToken, origin string) (*credentialSession, int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	m.removeExpired(now)
	session := m.sessions[id]
	if session == nil {
		return nil, http.StatusNotFound
	}
	if !secureEqual(session.accessToken, accessToken) {
		return nil, http.StatusUnauthorized
	}
	requestOrigin, err := normalizedOrigin(origin)
	if err != nil || requestOrigin != session.origin {
		return nil, http.StatusForbidden
	}
	delete(m.sessions, id)
	session.accessToken = ""
	return session, http.StatusOK
}

func (m *credentialManager) removeExpired(now time.Time) {
	for id, session := range m.sessions {
		if !now.Before(session.expiresAt) {
			delete(m.sessions, id)
		}
	}
}

func credentialTarget(token model.ConnectToken) (string, string, error) {
	protocol := strings.ToLower(strings.TrimSpace(token.Protocol))
	if protocol != "http" && protocol != "https" {
		return "", "", errors.New("connection token is not a Web protocol")
	}
	raw := strings.TrimSpace(token.Asset.Address)
	if raw == "" {
		return "", "", errors.New("Web asset address is empty")
	}
	if !strings.Contains(raw, "://") {
		raw = protocol + "://" + raw
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Hostname() == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.User != nil {
		return "", "", errors.New("Web asset address is invalid")
	}
	if parsed.Port() == "" {
		port := token.Asset.ProtocolPort(protocol)
		if port > 0 && !((parsed.Scheme == "http" && port == 80) || (parsed.Scheme == "https" && port == 443)) {
			parsed.Host = net.JoinHostPort(parsed.Hostname(), strconv.Itoa(port))
		}
	}
	parsed.Fragment = ""
	origin, err := normalizedOrigin(parsed.String())
	if err != nil {
		return "", "", errors.New("Web asset address is invalid")
	}
	return parsed.String(), origin, nil
}

func credentialSelectors(token model.ConnectToken) (string, string, string, bool) {
	mode := strings.ToLower(strings.TrimSpace(token.Asset.SpecInfo.Autofill))
	username := strings.TrimSpace(token.Asset.SpecInfo.UsernameSelector)
	password := strings.TrimSpace(token.Asset.SpecInfo.PasswordSelector)
	submit := strings.TrimSpace(token.Asset.SpecInfo.SubmitSelector)
	if mode == "" {
		if setting, ok := token.Platform.GetProtocolSetting(token.Protocol); ok {
			mode = strings.ToLower(stringSetting(setting.Setting, "autofill"))
			username = firstNonEmpty(username, stringSetting(setting.Setting, "username_selector"))
			password = firstNonEmpty(password, stringSetting(setting.Setting, "password_selector"))
			submit = firstNonEmpty(submit, stringSetting(setting.Setting, "submit_selector"))
		}
	}
	if mode != "basic" || (token.Account.SecretType.Value != "" && token.Account.SecretType.Value != "password") {
		return "", "", "", false
	}
	if (username != "" && !validCredentialSelector(username)) || !validCredentialSelector(password) || !validCredentialSelector(submit) {
		return "", "", "", false
	}
	if token.Account.Secret == "" {
		return "", "", "", false
	}
	return username, password, submit, true
}

func validCredentialSelector(selector string) bool {
	if selector == "" || len(selector) > 1024 {
		return false
	}
	parts := strings.SplitN(selector, "=", 2)
	if len(parts) != 2 || strings.TrimSpace(parts[1]) == "" {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(parts[0])) {
	case "name", "id", "type", "class_name", "css", "css_selector", "xpath":
		return true
	default:
		return false
	}
}

func stringSetting(setting map[string]any, key string) string {
	value, _ := setting[key].(string)
	return strings.TrimSpace(value)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func normalizedOrigin(value string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.User != nil {
		return "", errors.New("invalid Web origin")
	}
	hostname := strings.ToLower(parsed.Hostname())
	port := parsed.Port()
	if (parsed.Scheme == "http" && port == "80") || (parsed.Scheme == "https" && port == "443") {
		port = ""
	}
	host := hostname
	if port != "" {
		host = net.JoinHostPort(hostname, port)
	} else if strings.Contains(hostname, ":") {
		host = "[" + hostname + "]"
	}
	return strings.ToLower(parsed.Scheme) + "://" + host, nil
}

func secureEqual(left, right string) bool {
	if len(left) != len(right) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(left), []byte(right)) == 1
}

func randomCredentialValue(size int) (string, error) {
	data := make([]byte, size)
	if _, err := rand.Read(data); err != nil {
		return "", err
	}
	return hex.EncodeToString(data), nil
}

func sealCredentials(clientPublicKey, sessionID, origin string, credentials credentialEnvelope) (string, string, string, error) {
	clientDER, err := base64.StdEncoding.DecodeString(clientPublicKey)
	if err != nil {
		return "", "", "", err
	}
	parsedKey, err := x509.ParsePKIXPublicKey(clientDER)
	if err != nil {
		return "", "", "", err
	}
	clientKey, ok := parsedKey.(*ecdh.PublicKey)
	if !ok {
		return "", "", "", errors.New("client key is not X25519")
	}
	serverKey, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return "", "", "", err
	}
	sharedSecret, err := serverKey.ECDH(clientKey)
	if err != nil {
		return "", "", "", err
	}
	key, err := hkdf.Key(sha256.New, sharedSecret, nil, credentialKDFInfo, 32)
	clear(sharedSecret)
	if err != nil {
		return "", "", "", err
	}
	block, err := aes.NewCipher(key)
	clear(key)
	if err != nil {
		return "", "", "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", "", "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		return "", "", "", err
	}
	plaintext, err := json.Marshal(credentials)
	if err != nil {
		return "", "", "", err
	}
	ciphertext := gcm.Seal(nil, nonce, plaintext, []byte(sessionID+"\n"+origin))
	clear(plaintext)
	serverDER, err := x509.MarshalPKIXPublicKey(serverKey.PublicKey())
	if err != nil {
		return "", "", "", err
	}
	return base64.StdEncoding.EncodeToString(serverDER), base64.StdEncoding.EncodeToString(nonce), base64.StdEncoding.EncodeToString(ciphertext), nil
}

func writeCredentialJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
