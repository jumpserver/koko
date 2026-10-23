package session

import (
	"encoding/json"
	"testing"

	"github.com/jumpserver-dev/sdk-go/model"
	"github.com/jumpserver/koko/pkg/lion/guacd"
)

func TestRDPPrinting(t *testing.T) {
	for _, tc := range []struct {
		name     string
		options  string
		download bool
		want     string
	}{
		{"default", `{"resolution":"auto"}`, true, BoolFalse},
		{"disabled", `{"resolution":"auto","printers_redirect":false}`, true, BoolFalse},
		{"enabled", `{"resolution":"auto","printers_redirect":true}`, true, BoolTrue},
		{"download denied", `{"resolution":"auto","printers_redirect":true}`, false, BoolFalse},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var token connectTokenResponse
			if err := json.Unmarshal([]byte(`{"connect_options":`+tc.options+`}`), &token); err != nil {
				t.Fatal(err)
			}
			if token.Options.Resolution != "auto" {
				t.Fatal("SDK connection options were lost")
			}
			sess := TunnelSession{
				Asset: &model.Asset{}, Account: &model.Account{}, User: &model.User{},
				ActionPerm:     &ActionPermission{EnableDownload: tc.download},
				EnablePrinting: token.Options.PrintersRedirect,
			}
			for _, applet := range []bool{false, true} {
				if applet {
					sess.AppletOpts = &model.AppletOption{}
				}
				conf := sess.GuaConfiguration()
				if got := conf.GetParameter(guacd.RDPEnablePrinting); got != tc.want {
					t.Errorf("applet=%t: enable-printing = %q, want %q", applet, got, tc.want)
				}
			}
		})
	}
}
