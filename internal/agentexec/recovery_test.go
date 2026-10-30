package agentexec

import (
	"context"
	"testing"
	"time"

	"github.com/jumpserver/koko/internal/assetexec"
)

func TestRecoveryDoesNotExposeCapabilitiesOrRecreateConnections(t *testing.T) {
	p := Principal{ContextToken: "owner", UserID: "user", ClientID: "client"}
	r := &Runtime{sessions: map[string]*liveSession{}}
	r.core = coreFunc(func(_ context.Context, path string, body, out any) error {
		if path != "sessions/get/" || body.(map[string]any)["context_token"] != p.ContextToken {
			t.Fatal("recovery bypassed Core")
		}
		return respond(out, map[string]any{"session_id": "session", "status": "active", "session_token": "private", "requested": map[string]any{"secret": "private"}})
	})
	get := func() map[string]any {
		t.Helper()
		value, err := r.RecoverSession(context.Background(), p, "session", "")
		if err != nil {
			t.Fatal(err)
		}
		if value["session_token"] != nil || value["requested"] != nil || value["authorization_status"] != "active" {
			t.Fatal("private recovery capability exposed")
		}
		return value
	}
	if value := get(); value["native_connection_available"] != false || value["next_step"] != "create_session" || len(r.sessions) != 0 {
		t.Fatal("historical session restored a lost connection", value)
	}
	r.sessions["session"] = &liveSession{owner: "another-user", info: Session{Status: "active"}, connection: &assetexec.Connection{}}
	if value := get(); value["native_connection_available"] != false {
		t.Fatal("another owner's connection exposed")
	}
	r.sessions["session"].owner = owner(p)
	if value := get(); value["native_connection_available"] != true || value["next_step"] != "submit_operation" {
		t.Fatal("owned live connection unavailable", value)
	}
	r.sessions["session"].info.ExpiresAt = time.Now().Add(-time.Minute)
	if value := get(); value["native_connection_available"] != false || value["next_step"] != "create_session" {
		t.Fatal("expired connection appeared reusable", value)
	}
}
