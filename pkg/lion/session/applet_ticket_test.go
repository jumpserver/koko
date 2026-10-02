package session

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/jumpserver-dev/sdk-go/model"
	"github.com/jumpserver-dev/sdk-go/service"

	"github.com/jumpserver/koko/pkg/lion/guacd"
)

func TestAppletRDPConfiguration(t *testing.T) {
	// Match the Core applet-option response, including fields unknown to the SDK.
	const response = `{
		"id": "attempt-id",
		"host": {"address": "192.0.2.10", "protocols": [{"name": "rdp", "port": 3390}]},
		"account": {"id": "attempt-id", "username": "jlt_abcdefghijkl2345", "secret": "opaque_-TicketPassword"},
		"platform": {"protocols": [{"name": "rdp", "setting": {"ad_domain": "localhost", "security": "tls"}}]},
		"remote_app_option": {"remoteapplicationmode:i": "0", "alternate shell:s": ""}
	}`
	for _, tc := range []struct {
		name     string
		username string
		domain   string
		ticket   bool
	}{
		{name: "ticket", username: "jlt_abcdefghijkl2345", domain: "localhost", ticket: true},
		{name: "legacy", username: "applet-user"},
		{name: "legacy domain", username: `AD\applet-user`, domain: "AD"},
		{name: "prefix only", username: "jlt_admin"},
		{name: "wrong length", username: "jlt_abcdefghijkl234"},
		{name: "invalid character", username: "jlt_abcdefghijkl2348"},
		{name: "pool account", username: "jt_abcdefghijkl2345"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var option model.AppletOption
			if err := json.Unmarshal([]byte(response), &option); err != nil {
				t.Fatal(err)
			}
			option.Account.Username = tc.username
			// Even stale RemoteApp fields must not override the ticket shell flow.
			option.RemoteAppOption = model.RemoteAppCommandOption{
				Name: "legacy-app", CmdLine: "legacy-args", Shell: "legacy-shell",
			}
			sess := TunnelSession{
				AppletOpts: &option, Platform: option.Platform,
				User: &model.User{ID: "user-id"}, ActionPerm: &ActionPermission{},
			}
			conf := sess.GuaConfiguration()
			wantUsername := tc.username
			if tc.name == "legacy domain" {
				wantUsername = "applet-user@AD"
			}
			for key, want := range map[string]string{
				guacd.Hostname: "192.0.2.10", guacd.Port: "3390",
				guacd.RDPUsername: wantUsername, guacd.RDPPassword: option.Account.Secret,
				guacd.RDPDomain: tc.domain, guacd.RDPSecurity: "tls",
				guacd.RDPResizeMethod: "display-update",
			} {
				if got := conf.GetParameter(key); got != want {
					t.Errorf("%s = %q, want %q", key, got, want)
				}
			}
			if _, ok := conf.Parameters[guacd.RDPInitialProgram]; ok {
				t.Error("unexpected initial-program")
			}
			for key, want := range map[string]string{
				guacd.RDPRemoteApp: "legacy-app", guacd.RDPRemoteAppDir: "",
				guacd.RDPRemoteAppArgs: "legacy-args",
			} {
				got, exists := conf.Parameters[key]
				if tc.ticket && exists {
					t.Errorf("ticket must omit %s", key)
				} else if !tc.ticket && (!exists || got != want) {
					t.Errorf("legacy %s = %q, want %q", key, got, want)
				}
			}

			// Matching usernames on ordinary assets must not enter the applet flow.
			sess.AppletOpts = nil
			sess.Asset, sess.Account = &option.Host, &option.Account
			conf = sess.GuaConfiguration()
			if got := conf.GetParameter(guacd.RDPDomain); tc.ticket && got != "" {
				t.Errorf("ordinary RDP domain = %q, want empty", got)
			}
		})
	}
}

func TestAppletAccountRelease(t *testing.T) {
	released := make(chan string, 2)
	core := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != service.SuperConnectAppletHostAccountReleaseURL {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		var body struct {
			ID string `json:"id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		released <- body.ID
		w.WriteHeader(http.StatusNoContent)
	}))
	defer core.Close()
	jms, err := service.NewAuthJMService(service.JMSCoreHost(core.URL))
	if err != nil {
		t.Fatal(err)
	}
	server := Server{JmsService: jms}
	for _, tc := range []struct {
		name, username string
		wantRelease    bool
	}{
		{name: "ticket", username: "jlt_abcdefghijkl2345"},
		{name: "legacy", username: "applet-user", wantRelease: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
			ctx.Request = httptest.NewRequest(http.MethodGet, "/", nil)
			option := model.AppletOption{ID: tc.name + "-id"}
			option.Account.Username = tc.username
			sess, err := server.Create(ctx, WithProtocol(TypeRDP),
				ConnectTokenAuthInfo(&model.ConnectToken{}), WithUser(&model.User{}),
				WithAsset(&model.Asset{}), WithAccount(&model.Account{}), WithAppletOption(&option))
			if err != nil {
				t.Fatal(err)
			}
			if err := sess.ReleaseAppletAccount(); err != nil {
				t.Fatal(err)
			}
			select {
			case id := <-released:
				if !tc.wantRelease || id != option.ID {
					t.Errorf("unexpected account release: %s", id)
				}
			default:
				if tc.wantRelease {
					t.Error("legacy account was not released")
				}
			}
		})
	}
}
