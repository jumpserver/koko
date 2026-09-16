package httpd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/jumpserver-dev/sdk-go/model"
	"github.com/jumpserver-dev/sdk-go/service"
	"github.com/jumpserver/koko/pkg/auth"
	"github.com/jumpserver/koko/pkg/lion/tunnel"
)

func TestShareConnectionLifecycle(t *testing.T) {
	var joins atomic.Int32
	finished := make(chan struct{}, 3)
	core := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == service.TerminalConfigURL:
			_, _ = w.Write([]byte(`{"SECURITY_SESSION_SHARE":true}`))
		case r.Method == http.MethodPost && r.URL.Path == service.ShareSessionJoinURL:
			var params model.SharePostData
			if err := json.NewDecoder(r.Body).Decode(&params); err != nil || params.UserId != "viewer" || params.ShareId != "share" || params.Code != "1234" {
				t.Errorf("unexpected join parameters: %+v, %v", params, err)
			}
			joins.Add(1)
			_, _ = w.Write([]byte(`{"id":"record","session":{"id":"session"},"sharing":{"id":"share"}}`))
		case r.Method == http.MethodPatch && r.URL.Path == service.ShareSessionJoinURL+"record/finished/":
			finished <- struct{}{}
		case strings.Contains(r.URL.Path, "/sessions/"):
			http.NotFound(w, r)
		default:
			_, _ = w.Write([]byte(`{}`))
		}
	}))
	defer core.Close()
	client, err := service.NewAuthJMService(service.JMSCoreHost(core.URL))
	if err != nil {
		t.Fatal(err)
	}
	server := Server{
		apiClient:   client,
		broadCaster: &broadcaster{enterChannel: make(chan *UserWebsocket, 3), leavingChannel: make(chan *UserWebsocket, 3)},
		lionShare: &tunnel.GuacamoleTunnelServer{
			JmsService: client, Cache: &tunnel.GuaTunnelCacheManager{GuaTunnelCache: tunnel.NewLocalTunnelLocalCache()},
		},
	}
	router := gin.New()
	router.GET("/share", func(ctx *gin.Context) {
		ctx.Set(auth.ContextKeyUser, &model.User{ID: "viewer"})
		server.ProcessShareWebsocket(ctx)
	})
	host := httptest.NewServer(router)
	defer host.Close()
	for _, component := range []string{"", "unknown", "koko", "lion"} {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/share?target_id=share&code=1234&component="+component, nil))
		if w.Code != http.StatusBadRequest || joins.Load() != 0 {
			t.Fatal("an invalid component or failed WebSocket upgrade must not create a join")
		}
	}
	for i, component := range []string{"koko", "koko", "lion"} {
		conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(host.URL, "http")+"/share?target_id=share&code=1234&component="+component+"&joiner=other&record_id=other&type=connect&token=ignored", nil)
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()
		_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
		if _, _, err = conn.ReadMessage(); err != nil {
			t.Fatal(err)
		}
		if i == 1 { // A terminal initialization failure must also finish the join.
			frame, _ := buildEnvelope(envelopeTerminalCreate, []byte(`{"requestId":"create","params":{"rows":24,"cols":80}}`))
			_ = conn.WriteMessage(websocket.BinaryMessage, frame)
			_, _, _ = conn.ReadMessage()
		}
		if i == 0 {
			_ = conn.Close()
		}
		select {
		case <-finished:
		case <-time.After(3 * time.Second):
			t.Fatalf("%s did not finish its join", component)
		}
		if joins.Load() != int32(i+1) {
			t.Fatal("each connection must join exactly once")
		}
	}
}
