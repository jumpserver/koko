package proxy

import (
	"encoding/json"
	"fmt"
	"github.com/jumpserver/koko/pkg/session"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jumpserver-dev/sdk-go/model"
	"github.com/jumpserver-dev/sdk-go/service"
	"github.com/jumpserver/koko/pkg/i18n"
	"github.com/jumpserver/koko/pkg/zmodem"
)

func TestSubmitCommandReview(t *testing.T) {
	for _, tt := range []struct {
		name       string
		status     int
		body, want string
	}{
		{"workflow", 400, `["No eligible approvers. Select another reviewer."]`, "No eligible approvers. Select another reviewer."},
		{"permission", 403, `{"detail":"Permission denied."}`, "Permission denied."},
		{"transport", 0, `{}`, "Please contact your administrator."},
		{"server", 500, `{"detail":"private server trace"}`, "Please contact your administrator."},
		{"invalid_json", 400, `<html>private server trace</html>`, "Please contact your administrator."},
		{"invalid_success", 200, `{}`, "Please contact your administrator."},
		{"control_characters", 400, `["Reviewer\u001b[2J\nmissing"]`, "Reviewer [2J missing"},
		{"success", 200, `{"check_ticket_api":{"method":"GET","url":"/status/"},"assignees":["Reviewer"]}`, ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			core := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "POST" || r.URL.Path != service.AclCommandReviewURL || r.Header.Get("Accept-Language") != "zh-hans" {
					t.Errorf("unexpected request: %s %s %s", r.Method, r.URL.Path, r.Header.Get("Accept-Language"))
				}
				var data map[string]string
				if err := json.NewDecoder(r.Body).Decode(&data); err != nil || data["session_id"] != "session" || data["cmd_filter_acl_id"] != "acl" || data["run_command"] != "rm example" {
					t.Errorf("unexpected request body: %v, %v", data, err)
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tt.status)
				fmt.Fprint(w, tt.body)
			}))
			defer core.Close()
			api, err := service.NewAuthJMService(service.JMSCoreHost(core.URL))
			if err != nil {
				t.Fatal(err)
			}
			if tt.status == 0 {
				core.Close()
			}
			ticket, err := session.SubmitCommandReview(api, "session", "acl", "rm example", i18n.ZH)
			if tt.want == "" {
				if err != nil || ticket.CheckReq.URL != "/status/" {
					t.Fatalf("ticket = %+v, err = %v", ticket, err)
				}
			} else if err == nil || !strings.Contains(err.Error(), tt.want) || strings.Contains(err.Error(), "private server trace") || strings.Contains(err.Error(), "\x1b") {
				t.Fatalf("unexpected public error: %v", err)
			}
		})
	}
}

func TestParserCommandReviewOutcome(t *testing.T) {
	for _, tt := range []struct {
		name, state, wantOutput, wantInput string
		wantRisk                           int64
	}{
		{"submission_failure", "", "No eligible approvers.", "\x05\x15\r", model.ReviewCancel},
		{"rejected", "rejected", "Reviewer rejected", "\x05\x15\r", model.ReviewReject},
		{"approved", "approved", "Reviewer approved", "\r", model.ReviewAccept},
	} {
		t.Run(tt.name, func(t *testing.T) {
			core := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.Method == "POST" {
					if tt.state == "" {
						w.WriteHeader(400)
						fmt.Fprint(w, `["No eligible approvers."]`)
						return
					}
					fmt.Fprint(w, `{"check_ticket_api":{"method":"GET","url":"/status/"},"assignees":["Reviewer"]}`)
				} else {
					fmt.Fprintf(w, `{"state":"%s","processor":"Reviewer"}`, tt.state)
				}
			}))
			defer core.Close()
			api, err := service.NewAuthJMService(service.JMSCoreHost(core.URL))
			if err != nil {
				t.Fatal(err)
			}
			p := &Parser{
				id: "session", jmsService: api, i18nLang: "en",
				userOutputChan: make(chan []byte, 8), srvOutputChan: make(chan []byte, 64),
				zmodemParser: zmodem.New(), platform: &model.Platform{Name: "Linux"},
				cmdFilterACLs: model.CommandACLs{{ID: "acl", Action: model.ActionReview, CommandGroups: []model.CommandFilterItem{{RePattern: "^rm example$"}}}},
			}
			if err := p.initial(80, 24); err != nil {
				t.Fatal(err)
			}
			defer p.TerminalParser.Close()
			defer p.Close()
			p.TerminalParser.Feed([]byte("$ "))
			p.ParseUserInput([]byte("rm example"))
			p.TerminalParser.Feed([]byte("rm example"))
			if got := p.ParseUserInput([]byte("\r")); len(got) != 0 || !p.confirmStatus.InQuery() {
				t.Fatalf("command not held: %q", got)
			}
			p.ParseUserInput([]byte("y"))
			done := make(chan struct{})
			go func() { p.confirmStatus.wg.Wait(); close(done) }()
			select {
			case <-done:
			case <-time.After(12 * time.Second):
				t.Fatal("review did not finish")
			}
			var output strings.Builder
			for len(p.srvOutputChan) > 0 {
				output.Write(<-p.srvOutputChan)
			}
			if !strings.Contains(output.String(), tt.wantOutput) || (tt.state == "" && strings.Contains(output.String(), "rejected")) {
				t.Fatalf("output = %q", output.String())
			}
			risk := p.currentCmdRiskLevel
			if tt.state == "rejected" {
				select {
				case record := <-p.cmdRecordChan:
					risk = record.RiskLevel
				default:
					t.Fatal("missing rejected command audit")
				}
			}
			if risk != tt.wantRisk {
				t.Fatalf("risk = %d, want %d", risk, tt.wantRisk)
			}
			select {
			case got := <-p.userOutputChan:
				if string(got) != tt.wantInput {
					t.Fatalf("remote input = %q, want %q", got, tt.wantInput)
				}
			default:
				t.Fatal("missing remote input")
			}
			if p.confirmStatus.InRunning() || p.confirmStatus.InQuery() {
				t.Fatal("review still pending")
			}
		})
	}
}
