package terminalai

import (
	"fmt"
	"strings"
)

type SessionContext struct {
	Protocol     string `json:"protocol"`
	PlatformType string `json:"platformType,omitempty"`
	PlatformName string `json:"platformName,omitempty"`
	BaseOS       string `json:"baseOS,omitempty"`
	AssetName    string `json:"assetName,omitempty"`
	Database     string `json:"database,omitempty"`
}

type Adapter interface {
	Name() string
	Profile() AssetProfile
	SupportsBackground() bool
	PrepareProposal(*CommandProposal) error
}

type adapterFactory func(SessionContext) Adapter

var adapterFactories = map[string]adapterFactory{
	"ssh": func(context SessionContext) Adapter {
		if isShellContext(context) {
			return &shellAdapter{context: context}
		}
		return &terminalAdapter{context: context}
	},
	"mysql": func(context SessionContext) Adapter {
		return &mysqlAdapter{context: context}
	},
}

func ResolveAdapter(context SessionContext) Adapter {
	context.Protocol = strings.ToLower(strings.TrimSpace(context.Protocol))
	if factory, ok := adapterFactories[context.Protocol]; ok {
		return factory(context)
	}
	return &terminalAdapter{context: context}
}

type terminalAdapter struct {
	context SessionContext
}

func (a *terminalAdapter) Name() string {
	return "terminal"
}

func (a *terminalAdapter) Profile() AssetProfile {
	commandLanguage := "terminal input"
	if isShellContext(a.context) {
		commandLanguage = "POSIX shell command through the active PTY"
	}
	return newAdapterProfile(a.Name(), commandLanguage, a.context)
}

func (a *terminalAdapter) SupportsBackground() bool {
	return false
}

func (a *terminalAdapter) PrepareProposal(proposal *CommandProposal) error {
	if isInteractiveCommand(proposal.Command) {
		return fmt.Errorf("model generated an interactive or unbounded command")
	}
	if isShellContext(a.context) {
		proposal.RiskLevel, proposal.RiskReason = classifyRisk(
			proposal.Command, proposal.RiskLevel, proposal.RiskReason,
		)
	} else {
		proposal.RiskLevel, proposal.RiskReason = normalizeRisk(
			proposal.RiskLevel, proposal.RiskReason,
		)
	}
	proposal.BackgroundEligible = false
	return nil
}

func isShellContext(context SessionContext) bool {
	platformType := strings.TrimSpace(context.PlatformType)
	if platformType != "" {
		return strings.EqualFold(platformType, "linux") ||
			strings.EqualFold(platformType, "unix")
	}
	return strings.EqualFold(context.BaseOS, "linux") ||
		strings.EqualFold(context.BaseOS, "unix")
}

type shellAdapter struct {
	context SessionContext
}

func (a *shellAdapter) Name() string {
	return "ssh-shell"
}

func (a *shellAdapter) Profile() AssetProfile {
	return newAdapterProfile(a.Name(), "POSIX shell command", a.context)
}

func (a *shellAdapter) SupportsBackground() bool {
	return true
}

func (a *shellAdapter) PrepareProposal(proposal *CommandProposal) error {
	if isInteractiveCommand(proposal.Command) {
		return fmt.Errorf("model generated an interactive or unbounded command")
	}
	proposal.RiskLevel, proposal.RiskReason = classifyRisk(
		proposal.Command, proposal.RiskLevel, proposal.RiskReason,
	)
	proposal.BackgroundEligible = true
	return nil
}

type mysqlAdapter struct {
	context SessionContext
}

func (a *mysqlAdapter) Name() string {
	return "mysql"
}

func (a *mysqlAdapter) Profile() AssetProfile {
	return newAdapterProfile(a.Name(), "single MySQL statement", a.context)
}

func (a *mysqlAdapter) SupportsBackground() bool {
	return true
}

func (a *mysqlAdapter) PrepareProposal(proposal *CommandProposal) error {
	analysis, err := analyzeSQL(proposal.Command)
	if err != nil {
		return err
	}
	proposal.RiskLevel, proposal.RiskReason = classifySQLRisk(
		analysis, proposal.RiskLevel, proposal.RiskReason,
	)
	proposal.BackgroundEligible = analysis.BackgroundEligible()
	if proposal.Execution == ExecutionBackground && !proposal.BackgroundEligible {
		proposal.Execution = ExecutionPTY
		proposal.ExecutionCause = analysis.PTYReason()
	}
	return nil
}

func newAdapterProfile(name, commandLanguage string, context SessionContext) AssetProfile {
	return AssetProfile{
		Adapter:         name,
		CommandLanguage: commandLanguage,
		SessionContext:  context,
	}
}

func normalizeRisk(level int, reason string) (int, string) {
	if level < 1 || level > 4 {
		level = 2
	}
	if strings.TrimSpace(reason) == "" {
		reason = "risk classified by the terminal AI backend"
	}
	return level, reason
}
