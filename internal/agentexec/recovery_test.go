package agentexec

import (
	"context"
	"errors"
	"strings"
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

func TestExpiredResultsCannotEscapeThroughJournalOrFileTransfers(t *testing.T) {
	r := fileTestRuntime(t)
	if err := r.journal.PutAction(Action{ID: "expired", Status: "succeeded", Kind: "file_download", Result: map[string]any{"content": "retained"}}, "capability"); err != nil {
		t.Fatal(err)
	}
	r.core = coreFunc(func(_ context.Context, path string, _, _ any) error {
		return &CoreError{Status: 410, Operation: path, Code: "result_expired"}
	})
	ctx, p := context.Background(), Principal{ContextToken: "owner"}
	for _, read := range []func() error{
		func() error { _, err := r.Get(ctx, p, "expired", 0, 1); return err },
		func() error { _, err := r.Cancel(ctx, p, "expired"); return err },
		func() error { _, err := r.Upload(ctx, p, "expired", strings.NewReader(""), 0, ""); return err },
		func() error { _, _, _, err := r.Download(ctx, p, "expired"); return err },
	} {
		var expired *CoreError
		if err := read(); !errors.As(err, &expired) || expired.Code != "result_expired" {
			t.Fatal("expired Core result was served from local state", err)
		}
	}
}

func TestDisconnectUsesSessionAuthorityInsteadOfOldConnectionResult(t *testing.T) {
	r := fileTestRuntime(t)
	p := Principal{ContextToken: "owner", UserID: "user", ClientID: "client"}
	r.sessions = map[string]*liveSession{"session": {owner: owner(p), info: Session{ID: "session", ActionID: "old-connect", Status: "active"}}}
	r.core = coreFunc(func(_ context.Context, path string, _, out any) error {
		if path != "sessions/get/" {
			t.Fatal("disconnect tried to read an expired connection result", path)
		}
		return respond(out, map[string]any{"session_id": "session", "status": "active"})
	})
	result, err := r.Disconnect(context.Background(), p, "session")
	if err != nil || result["status"] != "closed" || len(r.sessions) != 0 {
		t.Fatal("active session could not be closed independently of old results", result, err)
	}
}
