package devcore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jumpserver-dev/sdk-go/model"

	"github.com/jumpserver/koko/pkg/common"
)

const (
	AccessKeyID     = "koko-dev"
	AccessKeySecret = "koko-dev-secret"
	TokenID         = "dev"

	defaultCorePort  = 18080
	defaultAssetPort = 22
)

type Config struct {
	CorePort  int
	AssetHost string
	AssetPort int
	AssetName string
	Username  string
	Password  string
	SFTPRoot  string
	AIBaseURL string
	AIAPIKey  string
	AIModel   string
	AIProxy   string
}

func LoadConfig() (Config, error) {
	corePort, err := envPort("KOKO_DEV_CORE_PORT", defaultCorePort)
	if err != nil {
		return Config{}, err
	}
	assetPort, err := envPort("KOKO_DEV_ASSET_PORT", defaultAssetPort)
	if err != nil {
		return Config{}, err
	}
	cfg := Config{
		CorePort:  corePort,
		AssetHost: strings.TrimSpace(os.Getenv("KOKO_DEV_ASSET_HOST")),
		AssetPort: assetPort,
		AssetName: strings.TrimSpace(os.Getenv("KOKO_DEV_ASSET_NAME")),
		Username:  strings.TrimSpace(os.Getenv("KOKO_DEV_ASSET_USERNAME")),
		Password:  os.Getenv("KOKO_DEV_ASSET_PASSWORD"),
		SFTPRoot:  strings.TrimSpace(os.Getenv("KOKO_DEV_SFTP_ROOT")),
		AIBaseURL: strings.TrimSpace(os.Getenv("KOKO_DEV_AI_BASE_URL")),
		AIAPIKey:  os.Getenv("KOKO_DEV_AI_API_KEY"),
		AIModel:   strings.TrimSpace(os.Getenv("KOKO_DEV_AI_MODEL")),
		AIProxy:   strings.TrimSpace(os.Getenv("KOKO_DEV_AI_PROXY")),
	}
	var missing []string
	if cfg.AssetHost == "" {
		missing = append(missing, "KOKO_DEV_ASSET_HOST")
	}
	if cfg.Username == "" {
		missing = append(missing, "KOKO_DEV_ASSET_USERNAME")
	}
	if cfg.Password == "" {
		missing = append(missing, "KOKO_DEV_ASSET_PASSWORD")
	}
	if len(missing) > 0 {
		return Config{}, fmt.Errorf("missing required development environment variables: %s",
			strings.Join(missing, ", "))
	}
	if cfg.AssetName == "" {
		cfg.AssetName = cfg.AssetHost
	}
	if cfg.SFTPRoot == "" {
		cfg.SFTPRoot = "home"
	}
	return cfg, nil
}

func envPort(name string, fallback int) (int, error) {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback, nil
	}
	port, err := strconv.Atoi(value)
	if err != nil || port < 1 || port > 65535 {
		return 0, fmt.Errorf("%s must be a valid TCP port", name)
	}
	return port, nil
}

type Server struct {
	server   *http.Server
	listener net.Listener
	cfg      Config
	user     model.User
	token    model.ConnectToken

	sessionsMu sync.RWMutex
	sessions   map[string]model.Session
}

func Start(cfg Config) (*Server, error) {
	listener, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(cfg.CorePort)))
	if err != nil {
		return nil, fmt.Errorf("start development core: %w", err)
	}
	s := &Server{
		listener: listener,
		cfg:      cfg,
		user: model.User{
			ID:       "koko-dev-user",
			Name:     "Koko Developer",
			Username: "developer",
			IsValid:  true,
			IsActive: true,
			Language: "en",
		},
		sessions: make(map[string]model.Session),
	}
	s.token = s.connectToken()
	s.server = &http.Server{Handler: s}
	go func() {
		_ = s.server.Serve(listener)
	}()
	return s, nil
}

func (s *Server) URL() string {
	return "http://" + s.listener.Addr().String()
}

func (s *Server) Close(ctx context.Context) error {
	return s.server.Shutdown(ctx)
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/api/v1/users/profile/":
		writeJSON(w, http.StatusOK, s.user)
	case r.Method == http.MethodGet && r.URL.Path == "/api/v1/settings/public/":
		writeJSON(w, http.StatusOK, map[string]any{
			"INTERFACE":              map[string]string{},
			"SECURITY_SESSION_SHARE": false,
			"VENDOR":                 "JumpServer",
		})
	case r.Method == http.MethodGet && r.URL.Path == "/api/v1/settings/i18n/koko/":
		writeJSON(w, http.StatusOK, map[string]any{})
	case r.Method == http.MethodGet && r.URL.Path == "/api/v1/terminal/terminals/config/":
		writeJSON(w, http.StatusOK, s.terminalConfig())
	case r.Method == http.MethodPost &&
		r.URL.Path == "/api/v1/authentication/super-connection-token/secret/":
		s.handleConnectToken(w, r)
	case r.Method == http.MethodGet &&
		r.URL.Path == "/api/v1/authentication/super-connection-token/"+TokenID+"/check/":
		writeJSON(w, http.StatusOK, model.TokenCheckStatus{
			Code: model.CodePermOk, Detail: TokenID,
		})
	case r.Method == http.MethodGet &&
		r.URL.Path == "/api/v1/perms/users/"+s.user.ID+"/assets/"+s.token.Asset.ID+"/":
		writeJSON(w, http.StatusOK, s.permAssetDetail())
	case r.URL.Path == "/api/v1/terminal/sessions/":
		s.handleSessions(w, r)
	case strings.HasPrefix(r.URL.Path, "/api/v1/terminal/sessions/"):
		s.handleSession(w, r)
	case r.Method == http.MethodPost && r.URL.Path == "/api/v1/terminal/commands/":
		writeJSON(w, http.StatusOK, map[string]any{})
	case r.Method == http.MethodPost && r.URL.Path == "/api/v1/audits/ftp-logs/":
		writeJSON(w, http.StatusOK, map[string]any{})
	case r.Method == http.MethodPost && r.URL.Path == "/api/v1/terminal/encrypted-config/":
		writeJSON(w, http.StatusOK, map[string]string{"value": ""})
	default:
		writeJSON(w, http.StatusNotImplemented, map[string]string{
			"detail": fmt.Sprintf("development core does not implement %s %s", r.Method, r.URL.Path),
		})
	}
}

func (s *Server) connectToken() model.ConnectToken {
	actions := model.Actions{
		{Label: "Connect", Value: model.ActionConnect},
		{Label: "Upload", Value: model.ActionUpload},
		{Label: "Download", Value: model.ActionDownload},
		{Label: "Copy", Value: model.ActionCopy},
		{Label: "Paste", Value: model.ActionPaste},
		{Label: "Delete", Value: model.ActionDelete},
	}
	protocols := []model.Protocol{
		{Name: model.ProtocolSSH, Port: s.cfg.AssetPort, Public: true},
		{Name: model.ProtocolSFTP, Port: s.cfg.AssetPort, Public: true},
	}
	platformProtocols := model.PlatformProtocols{
		{Protocol: protocols[0], Setting: map[string]any{}},
		{Protocol: protocols[1], Setting: map[string]any{
			"sftp_enabled": true,
			"sftp_home":    s.cfg.SFTPRoot,
		}},
	}
	return model.ConnectToken{
		Id:       TokenID,
		User:     s.user,
		Actions:  actions,
		Protocol: model.ProtocolSSH,
		OrgId:    "ROOT",
		OrgName:  "ROOT",
		ExpireAt: model.ExpireInfo(time.Now().Add(24 * time.Hour).Unix()),
		Account: model.Account{BaseAccount: model.BaseAccount{
			ID:       "koko-dev-account",
			Name:     s.cfg.Username,
			Username: s.cfg.Username,
			Secret:   s.cfg.Password,
			SecretType: model.LabelValue{
				Label: "Password",
				Value: "password",
			},
		}},
		Asset: model.Asset{
			ID:        "koko-dev-asset",
			Address:   s.cfg.AssetHost,
			Name:      s.cfg.AssetName,
			OrgID:     "ROOT",
			OrgName:   "ROOT",
			IsActive:  true,
			Protocols: protocols,
			Platform: model.BasePlatform{
				Name: "Linux",
				Type: "linux",
			},
		},
		Platform: model.Platform{
			BaseOs:    "linux",
			Name:      "Linux",
			Protocols: platformProtocols,
		},
		ConnectMethod: model.ConnectMethod{
			Component: "koko",
			Type:      "native",
			Label:     "Web Terminal",
			Value:     "web_terminal",
		},
	}
}

func (s *Server) terminalConfig() model.TerminalConfig {
	return model.TerminalConfig{
		PasswordAuth:        true,
		PublicKeyAuth:       false,
		ReplayStorage:       model.ReplayConfig{TypeName: "null"},
		CommandStorage:      model.CommandConfig{TypeName: "null"},
		MaxIdleTime:         30,
		MaxSessionTime:      24,
		HeartbeatDuration:   30,
		EnableSessionShare:  false,
		MaxStoreFTPFileSize: 0,
		GptBaseUrl:          s.cfg.AIBaseURL,
		GptApiKey:           s.cfg.AIAPIKey,
		GptModel:            s.cfg.AIModel,
		GptProxy:            s.cfg.AIProxy,
	}
}

func (s *Server) permAssetDetail() model.PermAssetDetail {
	return model.PermAssetDetail{
		ID:       s.token.Asset.ID,
		Name:     s.token.Asset.Name,
		Address:  s.token.Asset.Address,
		Platform: s.token.Asset.Platform,
		OrgID:    s.token.Asset.OrgID,
		OrgName:  s.token.Asset.OrgName,
		IsActive: true,
		Type:     model.LabelField("host"),
		Category: model.LabelField("host"),
		PermedProtocols: append(
			[]model.Protocol(nil), s.token.Asset.Protocols...,
		),
		PermedAccounts: []model.PermAccount{{
			Name:       s.token.Account.Name,
			Username:   s.token.Account.Username,
			SecretType: s.token.Account.SecretType.Value,
			HasSecret:  true,
			Actions:    append(model.Actions(nil), s.token.Actions...),
			Alias:      s.token.Account.Username,
		}},
	}
}

func (s *Server) handleConnectToken(w http.ResponseWriter, r *http.Request) {
	var request struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"detail": "invalid token request"})
		return
	}
	if request.ID != TokenID {
		writeJSON(w, http.StatusNotFound, map[string]string{
			"detail": "development connection token not found",
		})
		return
	}
	token := s.token
	token.ExpireAt = model.ExpireInfo(time.Now().Add(24 * time.Hour).Unix())
	writeJSON(w, http.StatusOK, token)
}

func (s *Server) handleSessions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"detail": "method not allowed"})
		return
	}
	var session model.Session
	if err := json.NewDecoder(r.Body).Decode(&session); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"detail": "invalid session"})
		return
	}
	if session.ID == "" {
		session.ID = common.UUID()
	}
	s.sessionsMu.Lock()
	s.sessions[session.ID] = session
	s.sessionsMu.Unlock()
	writeJSON(w, http.StatusCreated, session)
}

func (s *Server) handleSession(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/terminal/sessions/")
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) < 1 || parts[0] == "" {
		writeJSON(w, http.StatusNotFound, map[string]string{"detail": "session not found"})
		return
	}
	sessionID := parts[0]
	if len(parts) == 2 && parts[1] == "lifecycle_log" && r.Method == http.MethodPost {
		writeJSON(w, http.StatusOK, map[string]any{})
		return
	}
	s.sessionsMu.RLock()
	session, ok := s.sessions[sessionID]
	s.sessionsMu.RUnlock()
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"detail": "session not found"})
		return
	}
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, session)
	case http.MethodPatch:
		writeJSON(w, http.StatusOK, session)
	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"detail": "method not allowed"})
	}
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil && !errors.Is(err, net.ErrClosed) {
		return
	}
}
