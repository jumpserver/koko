package terminalai

import (
	"fmt"
	"strings"
	"sync"
)

type SessionContext struct {
	Protocol         string            `json:"protocol"`
	ConnectionMethod string            `json:"-"`
	AssetID          string            `json:"-"`
	AssetName        string            `json:"assetName,omitempty"`
	AssetAddress     string            `json:"-"`
	OrganizationID   string            `json:"-"`
	PlatformID       int               `json:"-"`
	PlatformCategory string            `json:"platformCategory,omitempty"`
	PlatformType     string            `json:"platformType,omitempty"`
	PlatformName     string            `json:"platformName,omitempty"`
	BaseOS           string            `json:"baseOS,omitempty"`
	Charset          string            `json:"charset,omitempty"`
	Database         string            `json:"database,omitempty"`
	NodeIDs          []string          `json:"-"`
	Labels           map[string]string `json:"-"`
	Attributes       map[string]string `json:"-"`
}

type Adapter interface {
	Name() string
	Profile() AssetProfile
	SupportsBackground() bool
	PrepareProposal(*CommandProposal) error
}

// ProfileDetectionAdapter is optional. Adapters implement it when their
// initial profile must be completed by a connection-time ProfileProvider.
type ProfileDetectionAdapter interface {
	NeedsProfileDetection() bool
}

func needsProfileDetection(adapter Adapter) bool {
	detector, ok := adapter.(ProfileDetectionAdapter)
	return ok && detector.NeedsProfileDetection()
}

func ResolveAdapter(context SessionContext) Adapter {
	context = normalizeSessionContext(context)
	var adapter Adapter
	if registration, ok := lookupProtocol(context.Protocol); ok {
		adapter = registration.NewAdapter(context)
	}
	if adapter == nil {
		adapter = &terminalAdapter{context: context}
	}
	return newRuleAdapter(adapter, context, currentRuleResolver())
}

type ruleAdapter struct {
	base     Adapter
	context  SessionContext
	resolver *ruleResolver

	mu         sync.RWMutex
	profile    AssetProfile
	resolution RuleResolution
}

func newRuleAdapter(
	base Adapter,
	context SessionContext,
	resolver *ruleResolver,
) *ruleAdapter {
	adapter := &ruleAdapter{
		base: base, context: context, resolver: resolver,
		profile: base.Profile(),
	}
	adapter.resolution = adapter.resolve(
		adapter.profile, rulePhaseConnection,
	)
	return adapter
}

func (a *ruleAdapter) Name() string {
	return a.base.Name()
}

func (a *ruleAdapter) Profile() AssetProfile {
	return a.base.Profile()
}

func (a *ruleAdapter) SupportsBackground() bool {
	a.mu.RLock()
	disabled := a.resolution.disablesBackground()
	a.mu.RUnlock()
	return a.base.SupportsBackground() && !disabled
}

func (a *ruleAdapter) NeedsProfileDetection() bool {
	return needsProfileDetection(a.base)
}

func (a *ruleAdapter) PrepareProposal(proposal *CommandProposal) error {
	before := *proposal
	if err := a.base.PrepareProposal(proposal); err != nil {
		return err
	}
	builtin := builtInCommandPolicy(a.base, before, *proposal)
	proposal.RuleMatches = append(proposal.RuleMatches, builtin.Matches...)
	a.mu.RLock()
	resolution := a.resolution
	a.mu.RUnlock()
	policy := resolution.commandPolicy(proposal.Command)
	proposal.RuleMatches = append(proposal.RuleMatches, policy.Matches...)
	proposal.RulePolicy = mergeCommandPolicies(builtin, policy)
	if policy.MinimumRisk > proposal.RiskLevel {
		proposal.RiskLevel, proposal.RiskReason = raiseRisk(
			proposal.RiskLevel,
			proposal.RiskReason,
			policy.MinimumRisk,
			ruleCause(policy.RiskSources),
		)
	}
	if policy.RequireApproval {
		proposal.ApprovalRequired = true
	}
	if policy.ForcePTY {
		proposal.Execution = ExecutionPTY
		proposal.ExecutionCause = ruleCause(policy.PTYSources)
		proposal.BackgroundEligible = false
	}
	if policy.MaxExecutionSeconds > 0 {
		proposal.MaxExecutionSeconds = policy.MaxExecutionSeconds
	}
	if policy.Deny {
		proposal.DeniedByRules = append(
			proposal.DeniedByRules, policy.DenySources...,
		)
		return fmt.Errorf(
			"command rejected by %s",
			ruleCause(policy.DenySources),
		)
	}
	return nil
}

func builtInCommandPolicy(
	base Adapter,
	before, after CommandProposal,
) RuleCommandPolicy {
	match := RuleMatch{
		ID: "builtin:adapter:" + base.Name(), Source: "builtin",
		Layer: "platform", Specificity: 30,
	}
	policy := RuleCommandPolicy{Matches: []RuleMatch{match}}
	if after.RiskLevel != before.RiskLevel ||
		after.RiskReason != before.RiskReason {
		riskMatch := match
		riskMatch.Reason = after.RiskReason
		policy.MinimumRisk = after.RiskLevel
		policy.RiskSources = append(policy.RiskSources, riskMatch)
	}
	if after.ApprovalRequired && !before.ApprovalRequired {
		approvalMatch := match
		approvalMatch.Reason = after.RiskReason
		policy.RequireApproval = true
		policy.ApprovalSources = append(
			policy.ApprovalSources, approvalMatch,
		)
	}
	if after.Execution == ExecutionPTY &&
		before.Execution != ExecutionPTY {
		ptyMatch := match
		ptyMatch.Reason = after.ExecutionCause
		policy.ForcePTY = true
		policy.PTYSources = append(policy.PTYSources, ptyMatch)
	}
	return policy
}

func mergeCommandPolicies(
	base RuleCommandPolicy,
	overlay RuleCommandPolicy,
) RuleCommandPolicy {
	result := base
	result.Matches = append(result.Matches, overlay.Matches...)
	if overlay.MinimumRisk > result.MinimumRisk {
		result.MinimumRisk = overlay.MinimumRisk
	}
	result.RequireApproval = result.RequireApproval || overlay.RequireApproval
	result.Deny = result.Deny || overlay.Deny
	result.ForcePTY = result.ForcePTY || overlay.ForcePTY
	if overlay.MaxExecutionSeconds > 0 &&
		(result.MaxExecutionSeconds == 0 ||
			overlay.MaxExecutionSeconds < result.MaxExecutionSeconds) {
		result.MaxExecutionSeconds = overlay.MaxExecutionSeconds
	}
	result.RiskSources = append(result.RiskSources, overlay.RiskSources...)
	result.ApprovalSources = append(
		result.ApprovalSources, overlay.ApprovalSources...,
	)
	result.DenySources = append(result.DenySources, overlay.DenySources...)
	result.PTYSources = append(result.PTYSources, overlay.PTYSources...)
	result.TimeoutSources = append(
		result.TimeoutSources, overlay.TimeoutSources...,
	)
	return result
}

func (a *ruleAdapter) UpdateProfile(profile AssetProfile) RuleResolution {
	a.mu.Lock()
	a.profile = profile
	a.resolution = a.resolve(profile, rulePhaseDetected)
	resolution := a.resolution
	a.mu.Unlock()
	return resolution
}

func (a *ruleAdapter) RuleResolution() RuleResolution {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.resolution
}

func (a *ruleAdapter) resolve(
	profile AssetProfile,
	phase string,
) RuleResolution {
	resolution := a.resolver.resolve(a.context, profile, phase)
	builtins := builtInRuleMatches(a.base, a.context)
	resolution.Matches = append(builtins, resolution.Matches...)
	return resolution
}

func builtInRuleMatches(base Adapter, context SessionContext) []RuleMatch {
	result := []RuleMatch{{
		ID: "builtin:terminal", Source: "builtin",
		Layer: "global", Specificity: 0,
	}}
	if context.Protocol != "" {
		result = append(result, RuleMatch{
			ID: "builtin:protocol:" + context.Protocol, Source: "builtin",
			Layer: "connection", Specificity: 20,
		})
	}
	if name := strings.TrimSpace(base.Name()); name != "" {
		result = append(result, RuleMatch{
			ID: "builtin:adapter:" + name, Source: "builtin",
			Layer: "platform", Specificity: 30,
		})
	}
	if family := strings.TrimSpace(base.Profile().PlatformFamily); family != "" {
		result = append(result, RuleMatch{
			ID: "builtin:platform:" + family, Source: "builtin",
			Layer: "platform", Specificity: 30,
		})
	}
	return result
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

func (a *shellAdapter) NeedsProfileDetection() bool {
	return true
}

func (a *shellAdapter) PrepareProposal(proposal *CommandProposal) error {
	if isInteractiveCommand(proposal.Command) {
		proposal.Execution = ExecutionPTY
		proposal.ExecutionCause = "interactive commands require the active PTY"
		proposal.BackgroundEligible = false
	} else {
		proposal.BackgroundEligible = true
	}
	proposal.RiskLevel, proposal.RiskReason = classifyRisk(
		proposal.Command, proposal.RiskLevel, proposal.RiskReason,
	)
	return nil
}

type sqlAdapter struct {
	context         SessionContext
	name            string
	commandLanguage string
}

func (a *sqlAdapter) Name() string {
	return a.name
}

func (a *sqlAdapter) Profile() AssetProfile {
	profile := newAdapterProfile(a.Name(), a.commandLanguage, a.context)
	profile.PlatformFamily = a.name
	return profile
}

func (a *sqlAdapter) SupportsBackground() bool {
	return true
}

func (a *sqlAdapter) PrepareProposal(proposal *CommandProposal) error {
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

type redisAdapter struct {
	context SessionContext
}

func (a *redisAdapter) Name() string {
	return "redis"
}

func (a *redisAdapter) Profile() AssetProfile {
	profile := newAdapterProfile(
		a.Name(), "single Redis command", a.context,
	)
	profile.PlatformFamily = a.Name()
	return profile
}

func (a *redisAdapter) SupportsBackground() bool {
	return true
}

func (a *redisAdapter) PrepareProposal(proposal *CommandProposal) error {
	arguments, err := parseRedisCommand(proposal.Command)
	if err != nil {
		return err
	}
	proposal.RiskLevel, proposal.RiskReason = normalizeRisk(
		proposal.RiskLevel, proposal.RiskReason,
	)
	classifyRedisProposal(arguments, proposal)
	proposal.BackgroundEligible = redisBackgroundEligible(arguments)
	if proposal.Execution == ExecutionBackground && !proposal.BackgroundEligible {
		proposal.Execution = ExecutionPTY
		proposal.ExecutionCause = "session-oriented or blocking Redis commands require the active PTY"
	}
	return nil
}

type mongoDBAdapter struct {
	context SessionContext
}

func (a *mongoDBAdapter) Name() string {
	return "mongodb"
}

func (a *mongoDBAdapter) Profile() AssetProfile {
	profile := newAdapterProfile(
		a.Name(),
		"single MongoDB shell expression; use db.runCommand with one strict Extended JSON object for background execution",
		a.context,
	)
	profile.PlatformFamily = a.Name()
	return profile
}

func (a *mongoDBAdapter) SupportsBackground() bool {
	return true
}

func (a *mongoDBAdapter) PrepareProposal(proposal *CommandProposal) error {
	proposal.RiskLevel, proposal.RiskReason = normalizeRisk(
		proposal.RiskLevel, proposal.RiskReason,
	)
	document, err := parseMongoDBCommand(proposal.Command)
	proposal.BackgroundEligible = err == nil && mongoDBBackgroundEligible(document)
	if err == nil {
		classifyMongoDBProposal(document, proposal)
	}
	if proposal.Execution == ExecutionBackground && !proposal.BackgroundEligible {
		proposal.Execution = ExecutionPTY
		proposal.ExecutionCause = "MongoDB background execution requires a finite, session-independent db.runCommand with strict Extended JSON"
	}
	return nil
}

func classifyRedisProposal(arguments []string, proposal *CommandProposal) {
	command := strings.ToUpper(arguments[0])
	switch command {
	case "GET", "MGET", "EXISTS", "TTL", "PTTL", "TYPE", "STRLEN",
		"GETRANGE", "KEYS", "SCAN", "HGET", "HGETALL", "HMGET", "HLEN",
		"HKEYS", "HVALS", "HEXISTS", "HRANDFIELD", "LLEN", "LRANGE",
		"LINDEX", "SCARD", "SMEMBERS", "SISMEMBER", "SMISMEMBER",
		"SRANDMEMBER", "ZCARD", "ZCOUNT", "ZLEXCOUNT", "ZRANGE",
		"ZRANGEBYSCORE", "ZRANK", "ZREVRANGE", "ZREVRANK", "ZSCORE",
		"ZMSCORE", "GEODIST", "GEOHASH", "GEOPOS", "GEOSEARCH", "PFCOUNT",
		"BITCOUNT", "BITPOS", "DBSIZE", "INFO", "LASTSAVE", "TIME", "ROLE",
		"COMMAND":
		return
	case "FLUSHALL", "FLUSHDB", "SHUTDOWN", "CONFIG", "ACL", "MODULE",
		"DEBUG", "REPLICAOF", "SLAVEOF", "FAILOVER", "MIGRATE", "RESTORE",
		"SWAPDB", "SCRIPT", "FUNCTION", "CLIENT":
		proposal.RiskLevel, proposal.RiskReason = raiseRisk(
			proposal.RiskLevel, proposal.RiskReason, 4,
			"backend rule detected a destructive or administrative Redis command",
		)
	default:
		proposal.RiskLevel, proposal.RiskReason = raiseRisk(
			proposal.RiskLevel, proposal.RiskReason, 2,
			"backend rule detected a potentially data-changing Redis command",
		)
	}
	proposal.ApprovalRequired = true
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
	context.ConnectionMethod = strings.ToLower(strings.TrimSpace(
		context.ConnectionMethod,
	))
	context.AssetID = strings.TrimSpace(context.AssetID)
	context.AssetName = strings.TrimSpace(context.AssetName)
	context.AssetAddress = strings.TrimSpace(context.AssetAddress)
	context.OrganizationID = strings.TrimSpace(context.OrganizationID)
	context.PlatformCategory = strings.TrimSpace(context.PlatformCategory)
	context.PlatformType = strings.TrimSpace(context.PlatformType)
	context.PlatformName = strings.TrimSpace(context.PlatformName)
	context.BaseOS = strings.TrimSpace(context.BaseOS)
	context.Charset = strings.TrimSpace(context.Charset)
	context.Database = strings.TrimSpace(context.Database)
	context.NodeIDs = normalizeStringList(context.NodeIDs)
	context.Labels = normalizeStringMap(context.Labels)
	context.Attributes = normalizeStringMap(context.Attributes)
	return context
}

func normalizeStringList(values []string) []string {
	result := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func normalizeStringMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	result := make(map[string]string, len(values))
	for key, value := range values {
		key = strings.ToLower(strings.TrimSpace(key))
		value = strings.TrimSpace(value)
		if key != "" && value != "" {
			result[key] = value
		}
	}
	return result
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
