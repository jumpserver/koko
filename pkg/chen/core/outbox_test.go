package core

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jumpserver-dev/sdk-go/model"
	"github.com/jumpserver-dev/sdk-go/service"
)

func TestCommandRetryAfterNotificationFailure(t *testing.T) {
	var saves, notifications atomic.Int32
	core := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case service.TerminalConfigURL:
		case service.SessionCommandURL:
			saves.Add(1)
		case service.NotificationCommandURL:
			if notifications.Add(1) == 1 {
				w.WriteHeader(http.StatusServiceUnavailable)
			}
		default:
			t.Errorf("unexpected request %s", r.URL.Path)
		}
		_, _ = w.Write([]byte("{}"))
	}))
	defer core.Close()
	client, err := service.NewAuthJMService(service.JMSCoreHost(core.URL))
	if err != nil {
		t.Fatal(err)
	}
	spool, err := newOutbox(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer spool.close()
	p := &CoreProvider{client: client, spool: spool}
	for _, risk := range []int64{model.WarningLevel, model.NormalLevel} {
		if err := spool.put(envelope{Command: &model.Command{SessionID: "session", RiskLevel: risk}}); err != nil {
			t.Fatal(err)
		}
	}
	p.flush(context.Background())
	if saves.Load() != 1 {
		t.Fatalf("first flush saved %d commands", saves.Load())
	}
	p.flush(context.Background())
	files, err := filepath.Glob(filepath.Join(spool.dir, "*.json"))
	if err != nil || len(files) != 0 || saves.Load() != 2 || notifications.Load() != 2 {
		t.Fatalf("retry left %d files, %d saves, %d notifications: %v", len(files), saves.Load(), notifications.Load(), err)
	}
}

func TestRecoverReplayInSessionStartDate(t *testing.T) {
	spool, err := newOutbox(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	replays, err := newReplayStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer spool.close()
	p := &CoreProvider{spool: spool, replays: replays}
	if err := p.recoverReplays(); err != nil {
		t.Fatal(err)
	}
	g := &Grant{ID: "session", created: time.Date(2024, 12, 31, 23, 59, 0, 0, time.UTC)}
	if err := replays.start(g); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := p.recoverReplays(); err != nil {
			t.Fatal(err)
		}
	}
	files, err := filepath.Glob(filepath.Join(spool.dir, "*.json"))
	if err != nil || len(files) != 1 {
		t.Fatalf("recovered %d entries: %v", len(files), err)
	}
	target, err := replayTarget(filepath.Join(replays.dir, "session.cast.gz"))
	if err != nil || target != "2024-12-31/session.cast.gz" {
		t.Fatalf("target %q: %v", target, err)
	}
}

func TestRejectLegacyOutboxAndConcurrentOwner(t *testing.T) {
	dir := t.TempDir()
	spool, err := newOutbox(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer spool.close()
	if other, err := newOutbox(dir); err == nil {
		other.close()
		t.Fatal("two owners accepted")
	}
	replays, err := newReplayStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	legacy := filepath.Join(dir, "pending.json")
	if err := os.WriteFile(legacy, []byte(`{"version":1,"forward":{"id":"old"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	p := &CoreProvider{spool: spool, replays: replays}
	if err := p.recoverReplays(); !errors.Is(err, errLegacyOutbox) {
		t.Fatalf("legacy cleanup: %v", err)
	}
	if err := os.Remove(legacy); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(replays.dir, "old.cast"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := p.recoverReplays(); !errors.Is(err, errLegacyOutbox) {
		t.Fatalf("legacy replay: %v", err)
	}
}
