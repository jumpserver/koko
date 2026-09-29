package proxy

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/jumpserver-dev/sdk-go/model"
	"github.com/jumpserver/koko/pkg/srvconn"
)

type winRMTestCommandStorage struct{ saved chan []*model.Command }

func (s winRMTestCommandStorage) TypeName() string { return "server" }
func (s winRMTestCommandStorage) BulkSave(commands []*model.Command) error {
	s.saved <- append([]*model.Command(nil), commands...)
	return nil
}

func TestCommandRecorderFlushesFinalQueue(t *testing.T) {
	saved := make(chan []*model.Command, 1)
	recorder := &CommandRecorder{queue: make(chan *model.Command, 2), closed: make(chan struct{}),
		storage: winRMTestCommandStorage{saved}}
	recorder.Record(&model.Command{Input: "Get-Location"})
	recorder.Record(&model.Command{Input: "Get-Service"})
	recorder.End()
	done := make(chan struct{})
	go func() { recorder.record(); close(done) }()
	select {
	case commands := <-saved:
		if len(commands) != 2 || commands[1].Input != "Get-Service" {
			t.Fatalf("final commands lost: %+v", commands)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("final commands were not saved")
	}
	<-done
}

func TestWinRMRejectedCommandIsAudited(t *testing.T) {
	recorder := &CommandRecorder{queue: make(chan *model.Command, 1)}
	server := &Server{ID: "session", account: &model.Account{BaseAccount: model.BaseAccount{Username: "operator"}}, backgroundRecorder: recorder,
		connOpts: &ConnectionOptions{authInfo: &model.ConnectToken{Protocol: srvconn.ProtocolWinRM,
			Asset: model.Asset{Name: "windows", OrgID: "org"}, ExpireAt: model.ExpireInfo(time.Now().Add(time.Hour).Unix())}},
		commandACLs: model.CommandACLs{{ID: "acl", Action: model.ActionReject, CommandGroups: []model.CommandFilterItem{{ID: "group", RePattern: `(?i)Restart-Service`}}}}}
	command := "Restart-Service Spooler; Write-Output '中文'"
	// A nil connection proves a rejected command never reaches the target.
	if err := server.executeWinRMCommand(nil, context.Background(), command, "shared user", io.Discard); err == nil {
		t.Fatal("ACL rejection was bypassed")
	}
	record := <-recorder.queue
	if record.Input != command || record.User != "shared user" || record.SessionID != "session" || record.OrgID != "org" ||
		record.RiskLevel != model.RejectLevel || record.CmdFilterAclId != "acl" || record.CmdGroupId != "group" || !strings.Contains(record.Output, "rejected") {
		t.Fatalf("incomplete command audit: %+v", record)
	}
}

func TestCommandReviewAuditLevels(t *testing.T) {
	for _, tc := range []struct {
		name     string
		rule     model.CommandAction
		decision model.CommandAction
		reviewed bool
		risk     int64
	}{
		{"reject", model.ActionReject, model.ActionReject, false, model.RejectLevel},
		{"review reject", model.ActionReview, model.ActionReject, false, model.ReviewReject},
		{"review cancel", model.ActionReview, model.ActionReview, false, model.ReviewCancel},
		{"review accept", model.ActionReview, model.ActionAccept, true, model.ReviewAccept},
	} {
		t.Run(tc.name, func(t *testing.T) {
			recorder := &CommandRecorder{queue: make(chan *model.Command, 1)}
			server := &Server{sessionInfo: &model.Session{ID: "session"}, backgroundRecorder: recorder,
				commandACLs: model.CommandACLs{{ID: "acl", Action: tc.rule}}}
			server.RecordBackgroundCommand("Get-Location", "audit output", nil, &CommandACLDecision{
				Action: tc.decision, ACLID: "acl", ItemID: "group", Reviewed: tc.reviewed,
			})
			record := <-recorder.queue
			if record.RiskLevel != tc.risk || record.CmdFilterAclId != "acl" || record.CmdGroupId != "group" {
				t.Fatalf("incorrect ACL audit: %+v", record)
			}
		})
	}
}
