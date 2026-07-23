package terminalai

import (
	"fmt"
	"strings"
)

type SessionContext struct {
	Protocol         string `json:"protocol"`
	AssetName        string `json:"assetName,omitempty"`
	PlatformCategory string `json:"platformCategory,omitempty"`
	PlatformType     string `json:"platformType,omitempty"`
	PlatformName     string `json:"platformName,omitempty"`
	BaseOS           string `json:"baseOS,omitempty"`
	Charset          string `json:"charset,omitempty"`
	Database         string `json:"database,omitempty"`
}

type Adapter interface {
	Name() string
	Profile() AssetProfile
	SupportsBackground() bool
	PrepareProposal(*CommandProposal) error
}

func ResolveAdapter(context SessionContext) Adapter {
	context = normalizeSessionContext(context)
	if registration, ok := lookupProtocol(context.Protocol); ok {
		if adapter := registration.NewAdapter(context); adapter != nil {
			return adapter
		}
	}
	return &terminalAdapter{context: context}
}

type terminalAdapter struct {
	context         SessionContext
	name            string
	platformFamily  string
	commandLanguage string
	sql             bool
}

func (a *terminalAdapter) Name() string {
	if a.name != "" {
		return a.name
	}
	return "terminal"
}

func (a *terminalAdapter) Profile() AssetProfile {
	commandLanguage := a.commandLanguage
	if commandLanguage == "" {
		commandLanguage = resolvePlatformCommand(a.context).Language
	}
	profile := newAdapterProfile(a.Name(), commandLanguage, a.context)
	if a.platformFamily != "" {
		profile.PlatformFamily = a.platformFamily
	}
	return profile
}

func (a *terminalAdapter) SupportsBackground() bool {
	return false
}

func (a *terminalAdapter) PrepareProposal(proposal *CommandProposal) error {
	if isInteractiveCommand(proposal.Command) {
		return fmt.Errorf("model generated an interactive or unbounded command")
	}
	if a.sql {
		analysis, err := analyzeSQL(proposal.Command)
		if err != nil {
			return err
		}
		if analysis.multi {
			return fmt.Errorf("model generated multiple SQL statements")
		}
		if analysis.incomplete {
			return fmt.Errorf("model generated incomplete SQL")
		}
		proposal.RiskLevel, proposal.RiskReason = classifySQLRisk(
			analysis, proposal.RiskLevel, proposal.RiskReason,
		)
		proposal.ApprovalRequired = analysis.RequiresApproval()
		if proposal.ApprovalRequired {
			proposal.RiskLevel, proposal.RiskReason = raiseRisk(
				proposal.RiskLevel, proposal.RiskReason, 2,
				"backend detected potentially data-changing SQL",
			)
		}
	} else if isShellContext(a.context) {
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
	return resolvePlatformCommand(context).Shell
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
	profile := newAdapterProfile(a.Name(), "single MySQL statement", a.context)
	profile.PlatformFamily = "mysql"
	return profile
}

func (a *mysqlAdapter) SupportsBackground() bool {
	return true
}

func (a *mysqlAdapter) PrepareProposal(proposal *CommandProposal) error {
	analysis, err := analyzeSQL(proposal.Command)
	if err != nil {
		return err
	}
	if analysis.multi {
		return fmt.Errorf("model generated multiple SQL statements")
	}
	if analysis.incomplete {
		return fmt.Errorf("model generated incomplete SQL")
	}
	proposal.RiskLevel, proposal.RiskReason = classifySQLRisk(
		analysis, proposal.RiskLevel, proposal.RiskReason,
	)
	proposal.ApprovalRequired = analysis.RequiresApproval()
	if proposal.ApprovalRequired {
		proposal.RiskLevel, proposal.RiskReason = raiseRisk(
			proposal.RiskLevel, proposal.RiskReason, 2,
			"backend detected potentially data-changing SQL",
		)
	}
	proposal.BackgroundEligible = analysis.BackgroundEligible()
	if proposal.Execution == ExecutionBackground && !proposal.BackgroundEligible {
		proposal.Execution = ExecutionPTY
		proposal.ExecutionCause = analysis.PTYReason()
	}
	return nil
}

func newAdapterProfile(name, commandLanguage string, context SessionContext) AssetProfile {
	platform := resolvePlatformCommand(context)
	return AssetProfile{
		Adapter:         name,
		CommandLanguage: commandLanguage,
		PlatformFamily:  platform.Family,
		SessionContext:  context,
	}
}

func normalizeSessionContext(context SessionContext) SessionContext {
	context.Protocol = strings.ToLower(strings.TrimSpace(context.Protocol))
	context.AssetName = strings.TrimSpace(context.AssetName)
	context.PlatformCategory = strings.TrimSpace(context.PlatformCategory)
	context.PlatformType = strings.TrimSpace(context.PlatformType)
	context.PlatformName = strings.TrimSpace(context.PlatformName)
	context.BaseOS = strings.TrimSpace(context.BaseOS)
	context.Charset = strings.TrimSpace(context.Charset)
	context.Database = strings.TrimSpace(context.Database)
	return context
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
