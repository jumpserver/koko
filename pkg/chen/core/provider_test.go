package core

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jumpserver-dev/sdk-go/model"
	"github.com/jumpserver-dev/sdk-go/service"
	"github.com/jumpserver/koko/pkg/session"
)

func TestManagedGrantUsesCoreSessionAndTerminalControls(t *testing.T) {
	var exchanges atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("GET "+service.TerminalConfigURL, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"SECURITY_MAX_IDLE_TIME":5,"SECURITY_MAX_SESSION_TIME":2}`)
	})
	mux.HandleFunc("POST "+service.SuperConnectTokenSecretURL, func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID     string
			Expire bool `json:"expire_now"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ID != "credential" || !req.Expire || exchanges.Add(1) != 1 {
			t.Error("connection token must be consumed once")
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"id":"token-id","protocol":"sqlserver","account":{"id":"account","username":"dbuser"},"user":{"id":"user"},"asset":{"id":"asset","address":"db.internal","protocols":[{"name":"sqlserver","port":1433}]},"expire_at":%d,"actions":[{"value":"connect"}],"command_filter_acls":[{"id":"sql-review","action":"review","command_groups":[{"id":"group","pattern":"^UPDATE"}]}]}`, time.Now().Add(time.Hour).Unix())
	})
	mux.HandleFunc("POST "+service.SessionListURL, func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Type      string `json:"type"`
			LoginFrom string `json:"login_from"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
		} else if req.Type != "db_workbench" || req.LoginFrom != "WT" {
			t.Errorf("Core session type = %q, login_from = %q", req.Type, req.LoginFrom)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"id":"core-session"}`)
	})
	mux.HandleFunc("POST "+service.AclCommandReviewURL, func(w http.ResponseWriter, r *http.Request) {
		var req map[string]string
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req["session_id"] != "core-session" || req["cmd_filter_acl_id"] != "sql-review" || req["run_command"] != "Affected rows: unknown\nUPDATE t SET n=1" {
			t.Error("review lost the authorized batch or session identity")
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"check_ticket_api":{"method":"GET","url":"/api/v1/tickets/review/"},"close_ticket_api":{"method":"DELETE","url":"/api/v1/tickets/review/"}}`)
	})
	mux.HandleFunc("GET /api/v1/tickets/review/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"state":"approved"}`)
	})
	core := httptest.NewServer(mux)
	defer core.Close()
	client, err := service.NewAuthJMService(service.JMSCoreHost(core.URL))
	if err != nil {
		t.Fatal(err)
	}
	p, err := NewCoreProvider(client, Options{SpoolDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = p.spool.close() })
	ctx := t.Context()
	if _, err = p.Open(ctx, "credential", ""); !errors.Is(err, ErrControlNotReady) {
		t.Fatalf("open before control channel: %v", err)
	}
	p.SetControlAvailable(true)
	grant, err := p.Open(ctx, "credential", "127.0.0.1:4321")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := p.Close(ctx, grant); err != nil {
			t.Error(err)
		}
	}()
	tracked, ok := session.GetSessionById("core-session")
	if !ok || tracked.TokenId != "token-id" || grant.MaxIdle != 5*time.Minute || !grant.Config.TLS.Enabled || grant.Config.TLS.ServerName != "db.internal" {
		t.Fatal("Core grant lost session identity, limits, or TLS settings")
	}
	decision, err := p.AuthorizeCommand(ctx, grant, AuthorizationRequest{SQL: "UPDATE t SET n=1", SubmitReview: true})
	if err != nil || decision.RiskLevel != model.ReviewAccept || decision.GroupID != "group" {
		t.Fatalf("approved review: %+v, %v", decision, err)
	}
	if err := tracked.HandleTask(&model.TerminalTask{Name: model.TaskLockSession}); err != nil {
		t.Fatal(err)
	}
	p.SetControlAvailable(false)
	p.SetControlAvailable(true)
	if !errors.Is(grant.Check(), ErrLocked) {
		t.Fatal("control reconnect cleared administrator lock")
	}
	if err := tracked.HandleTask(&model.TerminalTask{Name: model.TaskUnlockSession}); err != nil || grant.Check() != nil {
		t.Fatal("administrator unlock did not resume the session")
	}
	if err := tracked.HandleTask(&model.TerminalTask{Name: model.TaskKillSession}); err != nil || !errors.Is(grant.Check(), ErrClosed) {
		t.Fatal("administrator termination did not close the session")
	}
}
