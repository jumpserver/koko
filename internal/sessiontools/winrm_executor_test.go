package sessiontools

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/jumpserver/koko/pkg/srvconn"
)

func TestWinRMToolAuditsRejectedCommand(t *testing.T) {
	command := "Restart-Service Spooler"
	audits := 0
	tool, err := NewCommandTool(MCPCommandToolOptions{Protocol: srvconn.ProtocolWinRM,
		Executor: NewWinRMExecutor(nil), Validate: ProtocolCommandValidator(srvconn.ProtocolWinRM),
		Hooks: MCPCommandHooks{
			BackgroundAvailable: func() bool { return true },
			CommandACLCheck:     func(string) CommandACLDecision { return CommandACLDecision{Action: "reject", ACLID: "acl"} },
			BackgroundRecord: func(input, output string, exit *int, decision *CommandACLDecision) {
				audits++
				if input != command || !strings.Contains(output, "rejected") || exit != nil || decision.ACLID != "acl" {
					t.Fatalf("incomplete rejected command audit: %s %s %+v", input, output, decision)
				}
			},
		}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tool.Call(context.Background(), json.RawMessage(`{"command":"Restart-Service Spooler"}`)); err == nil || audits != 1 {
		t.Fatalf("rejected command was not audited once: %v (%d audits)", err, audits)
	}
	if _, exists := tool.Definition().Meta[MCPCommandPolicyMetaKey]; exists {
		t.Fatal("PowerShell must not advertise the POSIX read-only classifier")
	}
}

func TestWinRMToolRequiresRiskConfirmation(t *testing.T) {
	for _, mode := range []string{MCPExecutionAuto, MCPExecutionPTY, MCPExecutionBackground} {
		t.Run(mode, func(t *testing.T) {
			audits, executions := 0, 0
			tool, err := NewCommandTool(MCPCommandToolOptions{Protocol: srvconn.ProtocolWinRM,
				Executor: NewWinRMExecutor(nil), Validate: ProtocolCommandValidator(srvconn.ProtocolWinRM),
				Hooks: MCPCommandHooks{
					BackgroundAvailable: func() bool { return true },
					CommandACLCheck: func(string) CommandACLDecision {
						return CommandACLDecision{Action: "notify_and_warn", ACLID: "acl", ItemID: "group"}
					},
					PTYExecute: func(context.Context, string, *CommandACLDecision) (string, *int, error) {
						executions++
						return "", nil, nil
					},
					BackgroundRecord: func(input, output string, exit *int, decision *CommandACLDecision) {
						audits++
						if input != "Get-Location" || !strings.Contains(output, "risk confirmation") || exit != nil ||
							decision.ACLID != "acl" || decision.ItemID != "group" || decision.Action != "notify_and_warn" {
							t.Fatalf("incomplete warning audit: %s %s %+v", input, output, decision)
						}
					},
				}})
			if err != nil {
				t.Fatal(err)
			}
			_, err = tool.Call(context.Background(), json.RawMessage(`{"command":"Get-Location","execution":"`+mode+`"}`))
			if err == nil || audits != 1 || executions != 0 {
				t.Fatalf("unconfirmed warning executed: %v, executions=%d audits=%d", err, executions, audits)
			}
		})
	}
}

func TestWinRMForegroundModesAndAudit(t *testing.T) {
	for _, mode := range []string{MCPExecutionAuto, MCPExecutionPTY} {
		for _, failed := range []bool{false, true} {
			audits, executions := 0, 0
			tool, err := NewCommandTool(MCPCommandToolOptions{Protocol: srvconn.ProtocolWinRM,
				Validate: ProtocolCommandValidator(srvconn.ProtocolWinRM),
				Hooks: MCPCommandHooks{
					BackgroundAvailable: func() bool { return true },
					PTYExecute: func(context.Context, string, *CommandACLDecision) (string, *int, error) {
						executions++
						if failed {
							return "PS> still output", nil, errors.New("cancelled")
						}
						return "AI-PTY-OK", nil, nil
					},
					BackgroundRecord: func(input, output string, _ *int, _ *CommandACLDecision) {
						audits++
						if input != "Get-Location" || (failed && !strings.Contains(output, "cancelled")) || (!failed && output != "AI-PTY-OK") {
							t.Fatalf("incomplete foreground audit: %s %s", input, output)
						}
					},
				}})
			if err != nil {
				t.Fatal(err)
			}
			result, err := tool.Call(context.Background(), json.RawMessage(`{"command":"Get-Location","execution":"`+mode+`"}`))
			if (err != nil) != failed || audits != 1 || executions != 1 {
				t.Fatalf("%s failed=%t: %v, executions=%d audits=%d", mode, failed, err, executions, audits)
			}
			if !failed && result.(MCPCommandResult).Execution != MCPExecutionPTY {
				t.Fatalf("auto did not select the visible terminal: %+v", result)
			}
		}
	}
}
