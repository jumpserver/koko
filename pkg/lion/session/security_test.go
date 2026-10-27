package session

import (
	"testing"

	"github.com/jumpserver-dev/sdk-go/model"
	"github.com/jumpserver/koko/pkg/lion/guacd"
)

func TestRDPSecurity(t *testing.T) {
	for _, tc := range []struct {
		platform, option, want string
		ticket                 bool
	}{
		{"", "", "any", false},
		{"tls", "", "tls", false},
		{"tls", "default", "tls", false},
		{"tls", "any", "any", false},
		{"tls", "nla", "nla", false},
		{"any", "rdp", "rdp", false},
		{"nla", "tls", "tls", false},
		{"nla", "rdp", "tls", true},
	} {
		t.Run(tc.platform+"/"+tc.option, func(t *testing.T) {
			sess := TunnelSession{
				Asset: &model.Asset{}, Account: &model.Account{}, User: &model.User{},
				ActionPerm: &ActionPermission{},
				Platform: &model.Platform{Protocols: model.PlatformProtocols{{
					Protocol: model.Protocol{Name: "rdp"}, Setting: map[string]any{"security": tc.platform},
				}}},
				AuthInfo: &model.ConnectToken{ConnectOptions: model.ConnectOptions{RDPSecurity: tc.option}},
			}
			if tc.ticket {
				sess.AppletOpts = &model.AppletOption{}
				sess.AppletOpts.Account.Username = "jlt_abcdefghijkl2345"
			}
			conf := sess.GuaConfiguration()
			if got := conf.GetParameter(guacd.RDPSecurity); got != tc.want {
				t.Fatalf("security = %q, want %q", got, tc.want)
			}
		})
	}
}
