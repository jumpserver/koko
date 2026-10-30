package agentexec

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/jumpserver/koko/internal/assetexec"
)

type coreFunc func(context.Context, string, any, any) error

func (f coreFunc) Call(ctx context.Context, path string, body, out any) error {
	return f(ctx, path, body, out)
}
func respond(out, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, out)
}

func TestIntrospectionDeclaresExecutorCapabilities(t *testing.T) {
	r := &Runtime{core: coreFunc(func(_ context.Context, path string, body, out any) error {
		values := body.(map[string]any)
		capabilities := values["capabilities"].(map[string]any)
		protocols := capabilities["protocols"].(map[string]string)
		if path != "introspect/" || capabilities["version"] != 1 || protocols["ssh"] != "shell" || protocols["winrm"] != "" || protocols["sftp"] != "files" || len(protocols) != 10 {
			t.Fatalf("invalid executor capabilities: %v", capabilities)
		}
		for _, protocol := range []string{"mysql", "mariadb", "postgresql", "sqlserver", "oracle", "clickhouse", "redis", "mongodb"} {
			if protocols[protocol] != "database" {
				t.Fatalf("missing database executor capability: %s", protocol)
			}
		}
		delete(protocols, "ssh")
		return respond(out, Principal{ContextToken: "context", UserID: "user", OrgID: "org", ClientID: "client", Resource: "https://mcp.example/mcp", AllOrganizations: true})
	})}
	if _, err := r.Introspect(context.Background(), "token", "https://mcp.example/mcp"); err != nil {
		t.Fatal(err)
	}
	if assetexec.ProtocolMode("ssh") != "shell" || assetexec.ProtocolMode("unknown") != "" {
		t.Fatal("capability declaration changed the adapter registry")
	}
}

func TestHandoffClaimsOnceAndWaitsForRealTerminal(t *testing.T) {
	j, err := OpenJournal(t.TempDir(), 16<<20)
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	a := Action{ID: "handoff", ActionID: "handoff", SessionID: "session", Kind: "open_terminal", Status: "queued", Request: map[string]any{}}
	if err = j.PutAction(a, "capability"); err != nil {
		t.Fatal(err)
	}
	claims := 0
	link := "/core/custom-handoff/?action=handoff"
	r := &Runtime{journal: j, running: make(chan struct{}, 1)}
	r.core = coreFunc(func(ctx context.Context, path string, body, out any) error {
		switch {
		case strings.HasSuffix(path, "/authorize/"):
			return respond(out, a)
		case strings.HasSuffix(path, "/claim/"):
			claims++
			return respond(out, map[string]any{"id": a.ID, "status": "dispatching", "claimed": true, "handoff_url": link})
		default:
			t.Fatalf("unexpected Core operation %s", path)
			return nil
		}
	})
	p := Principal{OrgID: "org", UserID: "user", ClientID: "client"}
	s := &liveSession{owner: owner(p), info: Session{ID: "session", Status: "failed", SessionToken: "capability"}, gate: make(chan struct{}, 1)}
	r.sessions = map[string]*liveSession{"session": s}
	if _, err := r.getSession(p, s.info.ID, true); err == nil {
		t.Fatal("failed automatic connection accepted asset execution")
	}
	if _, err := r.getSession(p, s.info.ID, false); err != nil {
		t.Fatal("failed automatic connection blocked human handoff", err)
	}
	r.run(context.Background(), a, s)
	r.run(context.Background(), a, s)
	rec, err := j.Action(a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if claims != 1 || rec.Action.Status != "awaiting_input" || rec.Action.Result["requires_user"] != true {
		t.Fatalf("handoff falsely completed or replayed: claims=%d status=%s", claims, rec.Action.Status)
	}
	if got := rec.Action.Result["handoff_url"]; got != link {
		t.Fatalf("Core handoff link was not preserved: %v", got)
	}
	if got := normalizeToolResult(rec.Action, "https://jumpserver.test").(Action).Result["handoff_url"]; got != "https://jumpserver.test"+link {
		t.Fatalf("HTTP presentation did not complete the handoff link: %v", got)
	}
	events, err := j.Pending(8)
	if err != nil || len(events) != 1 || events[0].State != "awaiting_input" {
		t.Fatal("handoff emitted invalid execution evidence", err)
	}
	s.closing = true
	if _, err := r.getSession(p, s.info.ID, false); err == nil {
		t.Fatal("human handoff reopened a closing session")
	}
}

func TestCoreCancellationDoesNotProduceConflictingFinalEvent(t *testing.T) {
	j, err := OpenJournal(t.TempDir(), 16<<20)
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	a := Action{ID: "cancel", SessionID: "session", Kind: "execute_command", Status: "queued"}
	if err = j.PutAction(a, "capability"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r := &Runtime{journal: j, jobs: map[string]context.CancelFunc{a.ID: cancel}}
	r.core = coreFunc(func(_ context.Context, path string, body, out any) error {
		a.Status = "cancelled"
		return respond(out, a)
	})
	if _, err = r.Cancel(ctx, Principal{ContextToken: "owner"}, a.ID); err != nil {
		t.Fatal(err)
	}
	r.finish(a.ID, "rejected", map[string]any{"error": "worker observed cancellation"})
	events, err := j.Pending(8)
	if err != nil || len(events) != 0 {
		t.Fatal("Core cancellation was echoed as a conflicting event", err)
	}
	rec, _ := j.Action(a.ID)
	if rec.Action.Status != "cancelled" || ctx.Err() == nil {
		t.Fatal("cancellation did not preserve final state and stop worker")
	}
}

func TestSessionQueueLeavesCapacityForOtherSessions(t *testing.T) {
	r := fileTestRuntime(t)
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		r.running = make(chan struct{}, 1)
		r.core = coreFunc(func(_ context.Context, path string, _ any, out any) error {
			id := strings.Split(path, "/")[1]
			return respond(out, map[string]any{"id": id, "status": "queued", "claimed": true})
		})
		blocked := &liveSession{info: Session{ID: "busy"}, gate: make(chan struct{}, 1)}
		blocked.gate <- struct{}{}
		other := &liveSession{info: Session{ID: "other"}, gate: make(chan struct{}, 1)}
		for _, s := range []*liveSession{blocked, other} {
			a := Action{ID: s.info.ID, SessionID: s.info.ID, Kind: "open_terminal", Status: "queued"}
			if err := r.journal.PutAction(a, "capability"); err != nil {
				t.Fatal(err)
			}
			go r.run(ctx, a, s)
			synctest.Wait()
			if s == blocked && len(r.running) != 0 {
				t.Fatal("waiting on one session consumed global execution capacity")
			}
		}
		rec, err := r.journal.Action("other")
		if err != nil || rec.Action.Status != "awaiting_input" {
			t.Fatal("an unrelated session could not run while the first session was busy", err)
		}
		cancel()
		synctest.Wait()
	})
}

func TestFailedConnectionPreservesBoundedHandoffSession(t *testing.T) {
	r := fileTestRuntime(t)
	r.core = coreFunc(func(_ context.Context, path string, _ any, out any) error {
		if !strings.HasSuffix(path, "/check/") {
			t.Fatal("required human monitoring reached automatic credential release")
		}
		return respond(out, Session{Status: "awaiting_approval", RequiresTerminal: true})
	})
	p := Principal{OrgID: "org", UserID: "user", ClientID: "client"}
	s := &liveSession{owner: owner(p), info: Session{ID: "session", ActionID: "connect", ExpiresAt: time.Now().Add(time.Minute)}}
	r.sessions = map[string]*liveSession{s.info.ID: s}
	if err := r.journal.PutAction(Action{ID: "connect", Kind: "connect_asset", Status: "queued"}, "capability"); err != nil {
		t.Fatal(err)
	}
	r.openSession(context.Background(), s, "")
	if s.info.Status != "failed" || s.closing || s.connection != nil {
		t.Fatal("failed automatic connection discarded its logical handoff session")
	}
	if _, err := r.getSession(p, s.info.ID, true); err == nil {
		t.Fatal("failed connection accepted automatic execution")
	}
	if _, err := r.getSession(p, s.info.ID, false); err != nil {
		t.Fatal(err)
	}
	s.info.ExpiresAt = time.Now().Add(-time.Second)
	if _, err := r.getSession(p, s.info.ID, false); err == nil {
		t.Fatal("expired session accepted a human handoff")
	}
}

func TestExpiredSessionCannotExtendIdleLease(t *testing.T) {
	p := Principal{OrgID: "org", UserID: "user", ClientID: "client"}
	last := time.Now().Add(-time.Minute)
	s := &liveSession{owner: owner(p), info: Session{ID: "expired", Status: "active", ExpiresAt: time.Now().Add(-time.Second)}, lastUsed: last, connection: &assetexec.Connection{}}
	r := &Runtime{sessions: map[string]*liveSession{"expired": s}}
	if _, err := r.getSession(p, "expired", true); err == nil || !s.closing || s.lastUsed != last {
		t.Fatal("expired session was kept alive by another request")
	}
}

func TestConnectCancellationStopsOnlyMatchingAttempt(t *testing.T) {
	connecting, stopConnect := context.WithCancel(context.Background())
	defer stopConnect()
	other, stopOther := context.WithCancel(context.Background())
	defer stopOther()
	r := &Runtime{sessions: map[string]*liveSession{
		"session": {info: Session{ID: "session", ActionID: "connect"}, connectCancel: stopConnect},
		"other":   {info: Session{ID: "other", ActionID: "other-connect"}, connectCancel: stopOther},
	}, core: coreFunc(func(_ context.Context, path string, _ any, out any) error {
		if path != "actions/connect/cancel/" {
			t.Fatalf("unexpected cancellation path %s", path)
		}
		return respond(out, Action{ID: "connect", SessionID: "session", Kind: "connect_asset", Status: "dispatching", CancelRequested: true})
	})}
	if _, err := r.Cancel(context.Background(), Principal{ContextToken: "owner"}, "connect"); err != nil {
		t.Fatal(err)
	}
	if connecting.Err() != context.Canceled || other.Err() != nil {
		t.Fatal("connection cancellation was lost or stopped another session")
	}
}

func TestOutboxFailurePreservesEvidenceWithoutBlockingOtherActions(t *testing.T) {
	j, err := OpenJournal(t.TempDir(), 16<<20)
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	for _, id := range []string{"first", "second"} {
		if err = j.PutAction(Action{ID: id, Status: "queued"}, "capability"); err != nil {
			t.Fatal(err)
		}
		if id == "first" {
			for range 128 {
				if err = j.Append(id, "running", map[string]any{"data": "x"}); err != nil {
					t.Fatal(err)
				}
			}
		}
		if err = j.Append(id, "expired", map[string]any{}); err != nil {
			t.Fatal(err)
		}
	}
	var mu sync.Mutex
	delivered := map[string]int{}
	blocked := true
	r := &Runtime{journal: j, core: coreFunc(func(_ context.Context, path string, body, out any) error {
		if strings.Contains(path, "first") && blocked {
			return &CoreError{Status: 409, Operation: path}
		}
		mu.Lock()
		if body.(Event).Sequence != uint64(delivered[path]+1) {
			t.Fatal("audit delivery skipped an earlier event")
		}
		delivered[path]++
		mu.Unlock()
		return nil
	})}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	r.flush(ctx)
	pending, err := j.Pending(1024)
	if err != nil || len(pending) != 129 || pending[0].ActionID != "first" || len(delivered) != 1 {
		t.Fatal("outbox discarded evidence or blocked another operation", err)
	}
	blocked = false
	r.flush(ctx)
	r.flush(ctx)
	if pending, err = j.Pending(1024); err != nil || len(pending) != 0 || delivered["actions/first/events/"] != 129 {
		t.Fatal("recovered action did not deliver its original evidence in order", err)
	}
}

func TestOutboxDeadlineRotatesPastSlowAction(t *testing.T) {
	r := fileTestRuntime(t)
	for _, id := range []string{"first", "second"} {
		if err := r.journal.PutAction(Action{ID: id, Status: "queued"}, "capability"); err != nil {
			t.Fatal(err)
		}
		if err := r.journal.Append(id, "expired", nil); err != nil {
			t.Fatal(err)
		}
	}
	synctest.Test(t, func(t *testing.T) {
		healthy := 0
		r.core = coreFunc(func(ctx context.Context, path string, _, _ any) error {
			if strings.Contains(path, "first") {
				<-ctx.Done()
				return ctx.Err()
			}
			healthy++
			return nil
		})
		r.flush(context.Background())
		r.flush(context.Background())
		if healthy != 1 {
			t.Fatal("a slow action monopolized every delivery deadline")
		}
	})
}
