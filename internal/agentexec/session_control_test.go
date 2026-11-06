package agentexec

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"testing/synctest"

	"github.com/google/uuid"
	"github.com/jumpserver-dev/sdk-go/model"
	"github.com/jumpserver/koko/internal/assetexec"
	"github.com/jumpserver/koko/pkg/session"
)

func TestIdleAuthorityChecksRotateAndCloseOnlyRevokedConnections(t *testing.T) {
	r := fileTestRuntime(t)
	r.sessions = map[string]*liveSession{}
	connections := map[string]*assetexec.Connection{}
	for index := range 18 {
		id := fmt.Sprintf("session-%02d", index)
		connection := &assetexec.Connection{}
		connections[id] = connection
		r.sessions[id] = &liveSession{info: Session{ID: id, SessionToken: "capability"}, connection: connection}
	}
	checked := map[string]bool{}
	r.core = coreFunc(func(_ context.Context, path string, _ any, out any) error {
		id := strings.Split(path, "/")[1]
		checked[id] = true
		switch id {
		case "session-00":
			return &CoreError{Status: 403, Operation: path}
		case "session-01":
			return &CoreError{Status: 503, Operation: path}
		}
		return respond(out, Session{ID: id, Status: "ready"})
	})
	r.checkSessionAuthorities(context.Background())
	if len(checked) != 16 {
		t.Fatal("authority polling exceeded its batch bound")
	}
	r.checkSessionAuthorities(context.Background())
	if len(checked) != 18 || r.sessions["session-00"] != nil || r.sessions["session-01"] == nil {
		t.Fatal("polling starved a later session or treated an outage as revocation")
	}
	if _, err := connections["session-00"].Execute(context.Background(), assetexec.Request{}, nil); !errors.Is(err, assetexec.ErrContextLost) {
		t.Fatal("revocation did not close the idle transport")
	}
	if _, err := connections["session-01"].Execute(context.Background(), assetexec.Request{}, nil); errors.Is(err, assetexec.ErrContextLost) {
		t.Fatal("transient Core outage closed a valid transport")
	}
}

func TestQueuedAuthorityRevocationClosesTransportBeforeDispatch(t *testing.T) {
	for _, draining := range []bool{false, true} {
		t.Run(fmt.Sprintf("draining=%t", draining), func(t *testing.T) {
			r := fileTestRuntime(t)
			a := Action{ID: "queued", SessionID: "session", Kind: "execute_command", Status: "queued"}
			if err := r.journal.PutAction(a, "capability"); err != nil {
				t.Fatal(err)
			}
			synctest.Test(t, func(t *testing.T) {
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				connection := &assetexec.Connection{}
				s := &liveSession{info: Session{ID: "session", SessionToken: "capability", Status: "active"},
					connection: connection, active: 1, gate: make(chan struct{}, 1)}
				r.sessions = map[string]*liveSession{s.info.ID: s}
				r.jobs = map[string]context.CancelFunc{a.ID: cancel}
				r.running = make(chan struct{}, 1)
				r.running <- struct{}{}
				r.core = coreFunc(func(_ context.Context, path string, _ any, out any) error {
					if strings.HasSuffix(path, "/check/") {
						return &CoreError{Status: 403, Operation: path}
					}
					if !strings.HasSuffix(path, "/authorize/") {
						t.Fatalf("revoked queued work reached %s", path)
					}
					return respond(out, a)
				})
				done := make(chan struct{})
				go func() { defer close(done); r.run(ctx, a, s) }()
				synctest.Wait()
				if len(s.gate) != 1 {
					t.Fatal("operation did not wait on global execution capacity")
				}
				s.closing = draining
				r.checkSessionAuthorities(context.Background())
				<-done
				if ctx.Err() == nil || !s.closing {
					t.Fatal("queued operation escaped authorization revocation")
				}
				if _, err := connection.Execute(context.Background(), assetexec.Request{}, nil); !errors.Is(err, assetexec.ErrContextLost) {
					t.Fatal("revocation retained the queued session's transport")
				}
			})
		})
	}
}

func TestSessionControlRegistersHeartbeatAndCancelsOnlyItsJobs(t *testing.T) {
	r := fileTestRuntime(t)
	for id, owner := range map[string]string{"target-job": "target", "other-job": "other"} {
		if err := r.journal.PutAction(Action{ID: id, SessionID: owner, Status: "queued"}, "capability"); err != nil {
			t.Fatal(err)
		}
	}
	target, cancelTarget := context.WithCancel(context.Background())
	defer cancelTarget()
	other, cancelOther := context.WithCancel(context.Background())
	defer cancelOther()
	r.jobs = map[string]context.CancelFunc{"target-job": cancelTarget, "other-job": cancelOther}
	s := &liveSession{info: Session{ID: "target", Status: "active"}}
	id := uuid.NewString()
	r.registerSessionControl(s, model.Session{ID: id, TokenId: "connection-token"})
	defer session.RemoveSessionById(id)
	registered, ok := session.GetSessionById(id)
	if !ok || registered.TokenId != "connection-token" {
		t.Fatal("terminal/token registration missing")
	}
	online := false
	for _, value := range session.GetAliveSessionIds() {
		if value == id {
			online = true
		}
	}
	if !online {
		t.Fatal("heartbeat cannot see the agent terminal")
	}
	if err := registered.HandleTask(&model.TerminalTask{Name: model.TaskPermValid}); err != nil || s.closing {
		t.Fatal("valid permission changed session state")
	}
	if err := registered.HandleTask(&model.TerminalTask{Name: model.TaskKillSession, Args: id}); err != nil {
		t.Fatal(err)
	}
	if !s.closing || s.info.Status != "context_lost" || target.Err() == nil || other.Err() != nil {
		t.Fatal("administrator stop did not isolate and cancel the target session")
	}
	if err := registered.HandleTask(&model.TerminalTask{Name: model.TaskPermValid}); err != nil || s.info.Status != "context_lost" {
		t.Fatal("permission refresh resurrected lost context")
	}
}

func TestSessionControlPreservesSFTPHandlerAndToken(t *testing.T) {
	id := uuid.NewString()
	defer session.RemoveSessionById(id)
	called := false
	session.AddSession(session.NewSession(&model.Session{ID: id, TokenId: "sftp-token"}, func(task *model.TerminalTask) error { called = task.Name == model.TaskPermExpired; return nil }))
	r := &Runtime{}
	s := &liveSession{info: Session{ID: "sftp-agent", Status: "active"}}
	r.registerSessionControl(s, model.Session{ID: id})
	registered, _ := session.GetSessionById(id)
	if registered.TokenId != "sftp-token" {
		t.Fatal("SFTP token metadata was replaced")
	}
	if err := registered.HandleTask(&model.TerminalTask{Name: model.TaskPermExpired}); err != nil || !called || !s.closing || s.info.Status != "context_lost" {
		t.Fatal("SFTP control did not reach both existing and Agent session handling")
	}
}
