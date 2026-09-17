package httpd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/jumpserver-dev/sdk-go/model"
	"github.com/jumpserver-dev/sdk-go/service"
	"github.com/jumpserver/koko/pkg/auth"
	"github.com/jumpserver/koko/pkg/config"
	"github.com/jumpserver/koko/pkg/lion/session"
	"github.com/jumpserver/koko/pkg/lion/tunnel"
)

type monitorProbe struct{ *tunnel.GuaTunnelLocalCache }

func (m monitorProbe) Monitor(ctx *gin.Context) {
	_ = ctx.MustGet(config.GinCtxUserKey).(*model.User)
	ctx.Status(http.StatusNoContent)
}

func TestMonitorDispatch(t *testing.T) {
	previousConfig := config.GlobalConfig
	cfg := config.GetConf()
	config.GlobalConfig = &cfg
	t.Cleanup(func() { config.GlobalConfig = previousConfig })
	allowed := true
	core := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path != service.JoinRoomValidateURL {
			t.Errorf("unexpected permission URL: %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]bool{"ok": allowed})
	}))
	defer core.Close()
	jms, err := service.NewAuthJMService(service.JMSCoreHost(core.URL))
	if err != nil {
		t.Fatal(err)
	}
	cache := tunnel.NewLocalTunnelLocalCache()
	cache.Tunnels["graphical"] = &tunnel.Connection{Sess: &session.TunnelSession{ID: "graphical", Protocol: "http"}}
	server := Server{apiClient: jms, lionMonitor: monitorProbe{cache}}
	router := createRouter(jms, &server, nil)
	ticket := auth.ConnectTickets.Create(&model.User{ID: "viewer"}, nil, "", "")
	for _, tc := range []struct {
		sid       string
		allowed   bool
		component string
	}{
		{"graphical", true, "lion"},
		{"terminal", true, "koko"},
		{"graphical", false, ""},
	} {
		allowed = tc.allowed
		for _, ws := range []bool{false, true} {
			w := httptest.NewRecorder()
			path := "/koko/api/monitor/" + tc.sid + "/"
			if ws {
				path = "/koko/ws/monitor/"
			}
			req := httptest.NewRequest(http.MethodGet, path+"?target_id="+tc.sid+"&type=connect&token=ignored&ticket="+ticket.ID, nil)
			status := http.StatusOK
			if !tc.allowed {
				status = http.StatusForbidden
			} else if ws {
				status = http.StatusNoContent
				if tc.component == "koko" {
					// The real terminal handler rejects this request without a WebSocket handshake.
					status = http.StatusBadRequest
				}
			}
			router.ServeHTTP(w, req)
			if w.Code != status {
				t.Fatalf("session %s ws=%v: status %d, want %d", tc.sid, ws, w.Code, status)
			}
			if !ws && tc.allowed && w.Body.String() != `{"component":"`+tc.component+`"}` {
				t.Fatalf("session %s: response %s", tc.sid, w.Body.String())
			}
			if ws && tc.allowed {
				query := req.URL.Query()
				if query.Get("type") != TargetTypeMonitor || query.Get("target_id") != tc.sid || query.Has("token") {
					t.Fatalf("monitor must force read-only session parameters: %s", query.Encode())
				}
			}
		}
	}
}
