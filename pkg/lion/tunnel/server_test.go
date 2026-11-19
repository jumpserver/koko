package tunnel

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/jumpserver-dev/sdk-go/model"
	"github.com/jumpserver/koko/pkg/config"
	"github.com/jumpserver/koko/pkg/lion/session"
)

func TestDownloadRequiresPermission(t *testing.T) {
	user := &model.User{ID: "user"}
	cache := NewLocalTunnelLocalCache()
	cache.Tunnels["tunnel"] = &Connection{Sess: &session.TunnelSession{
		User: user, ActionPerm: &session.ActionPermission{},
	}}
	server := GuacamoleTunnelServer{Cache: &GuaTunnelCacheManager{GuaTunnelCache: cache}}
	response := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(response)
	ctx.Set(config.GinCtxUserKey, user)
	ctx.Params = gin.Params{{Key: "tid", Value: "tunnel"}}
	server.DownloadFile(ctx)
	if response.Code != http.StatusForbidden {
		t.Fatalf("download status = %d, want 403", response.Code)
	}
}
