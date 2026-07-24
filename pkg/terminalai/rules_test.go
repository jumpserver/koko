package terminalai

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestRuleResolutionLayeringAndPriority(t *testing.T) {
	resolver, err := compileRuleDocument("test", RuleDocument{
		Version: RuleSchemaVersion,
		Rules: []ContextRule{
			{
				ID: "asset", Priority: 1,
				Match: RuleMatcher{AssetIDs: []string{"asset-1"}},
				Enforce: RuleEnforcement{
					PromptAppend: []string{"asset constraint"},
				},
			},
			{
				ID: "protocol-high", Priority: 20,
				Match: RuleMatcher{Protocols: []string{"ssh"}},
				Enforce: RuleEnforcement{
					PromptAppend: []string{"protocol high"},
				},
			},
			{
				ID: "global", Priority: 100,
				Enforce: RuleEnforcement{
					PromptAppend: []string{"global constraint"},
				},
			},
			{
				ID: "protocol-low", Priority: 10,
				Match: RuleMatcher{Protocols: []string{"SSH"}},
				Enforce: RuleEnforcement{
					PromptAppend: []string{"protocol low"},
				},
			},
			{
				ID: "business", Priority: 1,
				Match: RuleMatcher{
					Labels: map[string]string{"environment": "prod*"},
				},
				Enforce: RuleEnforcement{
					PromptAppend: []string{"production constraint"},
				},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	resolution := resolver.resolve(SessionContext{
		Protocol: "ssh", AssetID: "asset-1",
		Labels: map[string]string{"environment": "production"},
	}, AssetProfile{}, rulePhaseConnection)
	var ids []string
	for _, match := range resolution.Matches {
		ids = append(ids, match.ID)
	}
	expected := []string{
		"global", "protocol-low", "protocol-high", "business", "asset",
	}
	if !reflect.DeepEqual(ids, expected) {
		t.Fatalf("match order = %#v, want %#v", ids, expected)
	}
	if !reflect.DeepEqual(resolution.PromptInstructions, []string{
		"global constraint", "protocol low", "protocol high",
		"production constraint", "asset constraint",
	}) {
		t.Fatalf("prompt order = %#v", resolution.PromptInstructions)
	}
}

func TestRuleResolutionRunsAgainWithDetectedProfile(t *testing.T) {
	resolver, err := compileRuleDocument("test", RuleDocument{
		Version: RuleSchemaVersion,
		Rules: []ContextRule{
			{
				ID: "ssh", Match: RuleMatcher{Protocols: []string{"ssh"}},
				Enforce: RuleEnforcement{
					PromptAppend: []string{"connection constraint"},
				},
			},
			{
				ID: "detected-linux",
				Match: RuleMatcher{
					OSIDs:             []string{"debian"},
					Shells:            []string{"*/bash"},
					AvailableCommands: []string{"systemctl"},
				},
				Enforce: RuleEnforcement{
					PromptAppend: []string{"detected constraint"},
					ForcePTY:     true,
				},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	context := SessionContext{Protocol: "ssh", BaseOS: "linux"}
	base := &shellAdapter{context: context}
	adapter := newRuleAdapter(base, context, resolver)
	initial := adapter.RuleResolution()
	if len(initial.PromptInstructions) != 1 ||
		initial.PromptInstructions[0] != "connection constraint" {
		t.Fatalf("initial resolution = %#v", initial)
	}
	detected := adapter.UpdateProfile(AssetProfile{
		Adapter: "ssh-shell", PlatformFamily: "linux",
		OSID: "debian", Shell: "/bin/bash",
		AvailableCommands: []string{"systemctl"},
		SessionContext:    context,
	})
	if !reflect.DeepEqual(detected.PromptInstructions, []string{
		"connection constraint", "detected constraint",
	}) {
		t.Fatalf("detected prompts = %#v", detected.PromptInstructions)
	}
	if adapter.SupportsBackground() {
		t.Fatal("detected PTY-only rule did not disable background execution")
	}
}

func TestBusinessRulesOnlyTightenBuiltInProposal(t *testing.T) {
	resolver, err := compileRuleDocument("test", RuleDocument{
		Version: RuleSchemaVersion,
		Rules: []ContextRule{{
			ID: "production-shell",
			Match: RuleMatcher{
				Protocols:    []string{"ssh"},
				CommandRegex: []string{`^echo\b`},
			},
			Enforce: RuleEnforcement{
				MinimumRisk: 4, RequireApproval: true,
				ForcePTY: true, MaxExecutionSeconds: 15,
				Reason: "production policy",
			},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	context := SessionContext{Protocol: "ssh", BaseOS: "linux"}
	adapter := newRuleAdapter(
		&shellAdapter{context: context}, context, resolver,
	)
	proposal := CommandProposal{
		Command: "echo ok", RiskLevel: 1,
		Execution: ExecutionBackground,
	}
	if err = adapter.PrepareProposal(&proposal); err != nil {
		t.Fatal(err)
	}
	if proposal.RiskLevel != 4 ||
		!proposal.ApprovalRequired ||
		proposal.Execution != ExecutionPTY ||
		proposal.BackgroundEligible ||
		proposal.MaxExecutionSeconds != 15 {
		t.Fatalf("tightened proposal = %#v", proposal)
	}
	found := false
	for _, match := range proposal.RuleMatches {
		if match.ID == "production-shell" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("rule matches = %#v", proposal.RuleMatches)
	}
}

func TestRuleCanRejectMatchingCommand(t *testing.T) {
	resolver, err := compileRuleDocument("test", RuleDocument{
		Version: RuleSchemaVersion,
		Rules: []ContextRule{{
			ID: "deny-reboot",
			Match: RuleMatcher{
				Protocols:    []string{"ssh"},
				CommandRegex: []string{`^reboot(?:\s|$)`},
			},
			Enforce: RuleEnforcement{Deny: true, Reason: "maintenance window"},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	context := SessionContext{Protocol: "ssh", BaseOS: "linux"}
	adapter := newRuleAdapter(
		&shellAdapter{context: context}, context, resolver,
	)
	proposal := CommandProposal{
		Command: "reboot", RiskLevel: 1, Execution: ExecutionPTY,
	}
	err = adapter.PrepareProposal(&proposal)
	if err == nil || !strings.Contains(err.Error(), "deny-reboot") {
		t.Fatalf("reject error = %v", err)
	}
	if len(proposal.DeniedByRules) != 1 {
		t.Fatalf("denied rules = %#v", proposal.DeniedByRules)
	}
}

func TestFileRuleProviderIsStrictAndAtomic(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "rules.yml")
	if err := os.WriteFile(path, []byte(
		"version: 1\nrules:\n  - id: invalid\n    unknown: true\n"+
			"    enforce:\n      force_pty: true\n",
	), 0600); err != nil {
		t.Fatal(err)
	}
	_, err := ConfigureRuleProvider(
		context.Background(), FileRuleProvider{Path: path},
	)
	if err == nil || !strings.Contains(err.Error(), "unknown") {
		t.Fatalf("strict load error = %v", err)
	}
}

func TestRulePromptIsAddedToTrustedSystemPolicy(t *testing.T) {
	client := &ModelClient{}
	client.SetPolicyInstructions([]string{"Do not modify production data."})
	system := client.withPolicy("base")
	if !strings.Contains(system, "administrator-configured") ||
		!strings.Contains(system, "Do not modify production data.") {
		t.Fatalf("system policy = %q", system)
	}
	client.SetPolicyInstructions(nil)
	if system = client.withPolicy("base"); system != "base" {
		t.Fatalf("cleared system policy = %q", system)
	}
}
