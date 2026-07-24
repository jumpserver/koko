package terminalai

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"
)

const (
	RuleSchemaVersion       = 1
	maxBusinessRules        = 1000
	maxRuleFileSize         = 1024 * 1024
	maxRuleID               = 256
	maxRuleMatcherValues    = 256
	maxRulePattern          = 2 * 1024
	maxRulePrompt           = 4 * 1024
	maxRulePromptTotal      = 16 * 1024
	maxRuleReason           = 2 * 1024
	maxRuleExecutionSeconds = 24 * 60 * 60

	rulePhaseConnection = "connection"
	rulePhaseDetected   = "detected"
)

// RuleDocument is the data returned by a rule provider. Version 1 rules are
// declarative safety overlays and cannot add protocol adapters or executors.
type RuleDocument struct {
	Version int           `json:"version" yaml:"version"`
	Rules   []ContextRule `json:"rules" yaml:"rules"`
}

type ContextRule struct {
	ID       string          `json:"id" yaml:"id"`
	Priority int             `json:"priority" yaml:"priority"`
	Match    RuleMatcher     `json:"match" yaml:"match"`
	Enforce  RuleEnforcement `json:"enforce" yaml:"enforce"`
}

type RuleMatcher struct {
	Protocols          []string          `json:"protocols,omitempty" yaml:"protocols,omitempty"`
	ConnectionMethods  []string          `json:"connection_methods,omitempty" yaml:"connection_methods,omitempty"`
	AssetIDs           []string          `json:"asset_ids,omitempty" yaml:"asset_ids,omitempty"`
	AssetNames         []string          `json:"asset_names,omitempty" yaml:"asset_names,omitempty"`
	AssetAddresses     []string          `json:"asset_addresses,omitempty" yaml:"asset_addresses,omitempty"`
	OrganizationIDs    []string          `json:"organization_ids,omitempty" yaml:"organization_ids,omitempty"`
	PlatformIDs        []int             `json:"platform_ids,omitempty" yaml:"platform_ids,omitempty"`
	PlatformCategories []string          `json:"platform_categories,omitempty" yaml:"platform_categories,omitempty"`
	PlatformTypes      []string          `json:"platform_types,omitempty" yaml:"platform_types,omitempty"`
	PlatformNames      []string          `json:"platform_names,omitempty" yaml:"platform_names,omitempty"`
	BaseOS             []string          `json:"base_os,omitempty" yaml:"base_os,omitempty"`
	Charsets           []string          `json:"charsets,omitempty" yaml:"charsets,omitempty"`
	Databases          []string          `json:"databases,omitempty" yaml:"databases,omitempty"`
	NodeIDs            []string          `json:"node_ids,omitempty" yaml:"node_ids,omitempty"`
	Labels             map[string]string `json:"labels,omitempty" yaml:"labels,omitempty"`
	Attributes         map[string]string `json:"attributes,omitempty" yaml:"attributes,omitempty"`
	Adapters           []string          `json:"adapters,omitempty" yaml:"adapters,omitempty"`
	PlatformFamilies   []string          `json:"platform_families,omitempty" yaml:"platform_families,omitempty"`
	OSNames            []string          `json:"os_names,omitempty" yaml:"os_names,omitempty"`
	OSIDs              []string          `json:"os_ids,omitempty" yaml:"os_ids,omitempty"`
	VersionIDs         []string          `json:"version_ids,omitempty" yaml:"version_ids,omitempty"`
	Kernels            []string          `json:"kernels,omitempty" yaml:"kernels,omitempty"`
	Architectures      []string          `json:"architectures,omitempty" yaml:"architectures,omitempty"`
	Shells             []string          `json:"shells,omitempty" yaml:"shells,omitempty"`
	AvailableCommands  []string          `json:"available_commands,omitempty" yaml:"available_commands,omitempty"`
	Capabilities       map[string]string `json:"capabilities,omitempty" yaml:"capabilities,omitempty"`
	CommandRegex       []string          `json:"command_regex,omitempty" yaml:"command_regex,omitempty"`
}

type RuleEnforcement struct {
	PromptAppend        []string `json:"prompt_append,omitempty" yaml:"prompt_append,omitempty"`
	MinimumRisk         int      `json:"minimum_risk,omitempty" yaml:"minimum_risk,omitempty"`
	RequireApproval     bool     `json:"require_approval,omitempty" yaml:"require_approval,omitempty"`
	Deny                bool     `json:"deny,omitempty" yaml:"deny,omitempty"`
	ForcePTY            bool     `json:"force_pty,omitempty" yaml:"force_pty,omitempty"`
	MaxExecutionSeconds int      `json:"max_execution_seconds,omitempty" yaml:"max_execution_seconds,omitempty"`
	Reason              string   `json:"reason,omitempty" yaml:"reason,omitempty"`
}

// RuleProvider keeps loading independent from matching. A future Core API
// provider can implement this interface without changing the rule resolver.
type RuleProvider interface {
	Name() string
	Load(context.Context) (RuleDocument, error)
}

type FileRuleProvider struct {
	Path string
}

func (p FileRuleProvider) Name() string {
	return "file"
}

func (p FileRuleProvider) Load(_ context.Context) (RuleDocument, error) {
	file, err := os.Open(strings.TrimSpace(p.Path))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return RuleDocument{Version: RuleSchemaVersion}, nil
		}
		return RuleDocument{}, err
	}
	defer file.Close()
	if info, statErr := file.Stat(); statErr != nil {
		return RuleDocument{}, statErr
	} else if info.Size() > maxRuleFileSize {
		return RuleDocument{}, fmt.Errorf(
			"rule file exceeds %d bytes", maxRuleFileSize,
		)
	}
	decoder := yaml.NewDecoder(io.LimitReader(file, maxRuleFileSize+1))
	decoder.KnownFields(true)
	var document RuleDocument
	if err = decoder.Decode(&document); err != nil {
		return RuleDocument{}, err
	}
	var extra any
	if err = decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			err = fmt.Errorf("multiple YAML documents are not supported")
		}
		return RuleDocument{}, err
	}
	return document, nil
}

type RuleMatch struct {
	ID          string `json:"id"`
	Source      string `json:"source"`
	Layer       string `json:"layer"`
	Priority    int    `json:"priority"`
	Specificity int    `json:"specificity"`
	Reason      string `json:"reason,omitempty"`
}

type RuleResolution struct {
	Phase              string      `json:"phase"`
	Matches            []RuleMatch `json:"matches"`
	PromptInstructions []string    `json:"promptInstructions,omitempty"`

	rules []*compiledRule
}

type RuleCommandPolicy struct {
	Matches             []RuleMatch `json:"matches,omitempty"`
	MinimumRisk         int         `json:"minimumRisk,omitempty"`
	RequireApproval     bool        `json:"requireApproval,omitempty"`
	Deny                bool        `json:"deny,omitempty"`
	ForcePTY            bool        `json:"forcePTY,omitempty"`
	MaxExecutionSeconds int         `json:"maxExecutionSeconds,omitempty"`
	RiskSources         []RuleMatch `json:"riskSources,omitempty"`
	ApprovalSources     []RuleMatch `json:"approvalSources,omitempty"`
	DenySources         []RuleMatch `json:"denySources,omitempty"`
	PTYSources          []RuleMatch `json:"ptySources,omitempty"`
	TimeoutSources      []RuleMatch `json:"timeoutSources,omitempty"`
}

type compiledRule struct {
	definition  ContextRule
	source      string
	layer       string
	specificity int
	matcher     compiledRuleMatcher
}

type compiledRuleMatcher struct {
	protocols          []*regexp.Regexp
	connectionMethods  []*regexp.Regexp
	assetIDs           []*regexp.Regexp
	assetNames         []*regexp.Regexp
	assetAddresses     []*regexp.Regexp
	organizationIDs    []*regexp.Regexp
	platformIDs        map[int]struct{}
	platformCategories []*regexp.Regexp
	platformTypes      []*regexp.Regexp
	platformNames      []*regexp.Regexp
	baseOS             []*regexp.Regexp
	charsets           []*regexp.Regexp
	databases          []*regexp.Regexp
	nodeIDs            []*regexp.Regexp
	labels             map[string]*regexp.Regexp
	attributes         map[string]*regexp.Regexp
	adapters           []*regexp.Regexp
	platformFamilies   []*regexp.Regexp
	osNames            []*regexp.Regexp
	osIDs              []*regexp.Regexp
	versionIDs         []*regexp.Regexp
	kernels            []*regexp.Regexp
	architectures      []*regexp.Regexp
	shells             []*regexp.Regexp
	availableCommands  []*regexp.Regexp
	capabilities       map[string]*regexp.Regexp
	commandRegex       []*regexp.Regexp
}

type ruleResolver struct {
	rules []*compiledRule
}

var configuredRules = struct {
	sync.RWMutex
	resolver *ruleResolver
}{resolver: &ruleResolver{}}

func ConfigureRuleProvider(ctx context.Context, provider RuleProvider) (int, error) {
	if provider == nil {
		return 0, fmt.Errorf("terminal AI rule provider is required")
	}
	document, err := provider.Load(ctx)
	if err != nil {
		return 0, fmt.Errorf("load terminal AI rules from %s: %w", provider.Name(), err)
	}
	resolver, err := compileRuleDocument(provider.Name(), document)
	if err != nil {
		return 0, fmt.Errorf("validate terminal AI rules from %s: %w", provider.Name(), err)
	}
	configuredRules.Lock()
	configuredRules.resolver = resolver
	configuredRules.Unlock()
	return len(resolver.rules), nil
}

func ConfigureRulesFile(ctx context.Context, path string) (int, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		configuredRules.Lock()
		configuredRules.resolver = &ruleResolver{}
		configuredRules.Unlock()
		return 0, nil
	}
	return ConfigureRuleProvider(ctx, FileRuleProvider{Path: path})
}

func currentRuleResolver() *ruleResolver {
	configuredRules.RLock()
	resolver := configuredRules.resolver
	configuredRules.RUnlock()
	if resolver == nil {
		return &ruleResolver{}
	}
	return resolver
}

func compileRuleDocument(source string, document RuleDocument) (*ruleResolver, error) {
	if document.Version != RuleSchemaVersion {
		return nil, fmt.Errorf(
			"unsupported schema version %d, expected %d",
			document.Version, RuleSchemaVersion,
		)
	}
	if len(document.Rules) > maxBusinessRules {
		return nil, fmt.Errorf("rule count exceeds %d", maxBusinessRules)
	}
	seen := make(map[string]struct{}, len(document.Rules))
	resolver := &ruleResolver{rules: make([]*compiledRule, 0, len(document.Rules))}
	totalPrompt := 0
	for index := range document.Rules {
		rule := document.Rules[index]
		rule.ID = strings.TrimSpace(rule.ID)
		if rule.ID == "" {
			return nil, fmt.Errorf("rule %d has no id", index+1)
		}
		if len(rule.ID) > maxRuleID {
			return nil, fmt.Errorf("rule %d id is too large", index+1)
		}
		if _, exists := seen[rule.ID]; exists {
			return nil, fmt.Errorf("duplicate rule id %q", rule.ID)
		}
		seen[rule.ID] = struct{}{}
		if err := validateRuleEnforcement(rule.ID, &rule.Enforce); err != nil {
			return nil, err
		}
		for _, prompt := range rule.Enforce.PromptAppend {
			totalPrompt += len(prompt)
		}
		if totalPrompt > maxRulePromptTotal {
			return nil, fmt.Errorf(
				"all rule prompts exceed %d bytes", maxRulePromptTotal,
			)
		}
		matcher, err := compileRuleMatcher(rule.ID, rule.Match)
		if err != nil {
			return nil, err
		}
		layer, specificity := ruleLayer(rule.Match)
		resolver.rules = append(resolver.rules, &compiledRule{
			definition:  rule,
			source:      strings.TrimSpace(source),
			layer:       layer,
			specificity: specificity,
			matcher:     matcher,
		})
	}
	sort.SliceStable(resolver.rules, func(i, j int) bool {
		left, right := resolver.rules[i], resolver.rules[j]
		if left.specificity != right.specificity {
			return left.specificity < right.specificity
		}
		if left.definition.Priority != right.definition.Priority {
			return left.definition.Priority < right.definition.Priority
		}
		return left.definition.ID < right.definition.ID
	})
	return resolver, nil
}

func validateRuleEnforcement(id string, enforcement *RuleEnforcement) error {
	enforcement.Reason = strings.TrimSpace(enforcement.Reason)
	if len(enforcement.Reason) > maxRuleReason {
		return fmt.Errorf("rule %q reason is too large", id)
	}
	if enforcement.MinimumRisk < 0 || enforcement.MinimumRisk > 4 {
		return fmt.Errorf("rule %q minimum_risk must be between 1 and 4", id)
	}
	if enforcement.MaxExecutionSeconds < 0 ||
		enforcement.MaxExecutionSeconds > maxRuleExecutionSeconds {
		return fmt.Errorf(
			"rule %q max_execution_seconds must be between 1 and %d",
			id, maxRuleExecutionSeconds,
		)
	}
	total := 0
	for index := range enforcement.PromptAppend {
		enforcement.PromptAppend[index] = strings.TrimSpace(
			enforcement.PromptAppend[index],
		)
		if enforcement.PromptAppend[index] == "" {
			return fmt.Errorf("rule %q contains an empty prompt", id)
		}
		if len(enforcement.PromptAppend[index]) > maxRulePrompt {
			return fmt.Errorf("rule %q prompt is too large", id)
		}
		total += len(enforcement.PromptAppend[index])
	}
	if total > maxRulePromptTotal {
		return fmt.Errorf("rule %q prompts exceed %d bytes", id, maxRulePromptTotal)
	}
	if len(enforcement.PromptAppend) == 0 &&
		enforcement.MinimumRisk == 0 &&
		!enforcement.RequireApproval &&
		!enforcement.Deny &&
		!enforcement.ForcePTY &&
		enforcement.MaxExecutionSeconds == 0 {
		return fmt.Errorf("rule %q has no enforcement", id)
	}
	return nil
}

func compileRuleMatcher(id string, matcher RuleMatcher) (compiledRuleMatcher, error) {
	var result compiledRuleMatcher
	var err error
	fields := []struct {
		name   string
		values []string
		target *[]*regexp.Regexp
	}{
		{"protocols", matcher.Protocols, &result.protocols},
		{"connection_methods", matcher.ConnectionMethods, &result.connectionMethods},
		{"asset_ids", matcher.AssetIDs, &result.assetIDs},
		{"asset_names", matcher.AssetNames, &result.assetNames},
		{"asset_addresses", matcher.AssetAddresses, &result.assetAddresses},
		{"organization_ids", matcher.OrganizationIDs, &result.organizationIDs},
		{"platform_categories", matcher.PlatformCategories, &result.platformCategories},
		{"platform_types", matcher.PlatformTypes, &result.platformTypes},
		{"platform_names", matcher.PlatformNames, &result.platformNames},
		{"base_os", matcher.BaseOS, &result.baseOS},
		{"charsets", matcher.Charsets, &result.charsets},
		{"databases", matcher.Databases, &result.databases},
		{"node_ids", matcher.NodeIDs, &result.nodeIDs},
		{"adapters", matcher.Adapters, &result.adapters},
		{"platform_families", matcher.PlatformFamilies, &result.platformFamilies},
		{"os_names", matcher.OSNames, &result.osNames},
		{"os_ids", matcher.OSIDs, &result.osIDs},
		{"version_ids", matcher.VersionIDs, &result.versionIDs},
		{"kernels", matcher.Kernels, &result.kernels},
		{"architectures", matcher.Architectures, &result.architectures},
		{"shells", matcher.Shells, &result.shells},
		{"available_commands", matcher.AvailableCommands, &result.availableCommands},
	}
	for _, field := range fields {
		*field.target, err = compileGlobPatterns(field.values)
		if err != nil {
			return result, fmt.Errorf(
				"rule %q matcher %s: %w", id, field.name, err,
			)
		}
	}
	result.labels, err = compileMapPatterns(matcher.Labels)
	if err != nil {
		return result, fmt.Errorf("rule %q matcher labels: %w", id, err)
	}
	result.attributes, err = compileMapPatterns(matcher.Attributes)
	if err != nil {
		return result, fmt.Errorf("rule %q matcher attributes: %w", id, err)
	}
	result.capabilities, err = compileMapPatterns(matcher.Capabilities)
	if err != nil {
		return result, fmt.Errorf("rule %q matcher capabilities: %w", id, err)
	}
	if len(matcher.PlatformIDs) > 0 {
		if len(matcher.PlatformIDs) > maxRuleMatcherValues {
			return result, fmt.Errorf(
				"rule %q matcher platform_ids has too many values", id,
			)
		}
		result.platformIDs = make(map[int]struct{}, len(matcher.PlatformIDs))
		for _, value := range matcher.PlatformIDs {
			result.platformIDs[value] = struct{}{}
		}
	}
	if len(matcher.CommandRegex) > maxRuleMatcherValues {
		return result, fmt.Errorf(
			"rule %q matcher command_regex has too many values", id,
		)
	}
	for _, pattern := range matcher.CommandRegex {
		pattern = strings.TrimSpace(pattern)
		if pattern == "" {
			return result, fmt.Errorf("rule %q contains an empty command regex", id)
		}
		if len(pattern) > maxRulePattern {
			return result, fmt.Errorf("rule %q command regex is too large", id)
		}
		compiled, compileErr := regexp.Compile("(?i:" + pattern + ")")
		if compileErr != nil {
			return result, fmt.Errorf(
				"rule %q command regex %q: %w", id, pattern, compileErr,
			)
		}
		result.commandRegex = append(result.commandRegex, compiled)
	}
	return result, nil
}

func compileGlobPatterns(values []string) ([]*regexp.Regexp, error) {
	if len(values) > maxRuleMatcherValues {
		return nil, fmt.Errorf("too many patterns")
	}
	result := make([]*regexp.Regexp, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			return nil, fmt.Errorf("empty pattern")
		}
		if len(value) > maxRulePattern {
			return nil, fmt.Errorf("pattern is too large")
		}
		var expression strings.Builder
		expression.WriteString("(?i)^")
		for _, character := range value {
			switch character {
			case '*':
				expression.WriteString(".*")
			case '?':
				expression.WriteRune('.')
			default:
				expression.WriteString(regexp.QuoteMeta(string(character)))
			}
		}
		expression.WriteRune('$')
		compiled, err := regexp.Compile(expression.String())
		if err != nil {
			return nil, err
		}
		result = append(result, compiled)
	}
	return result, nil
}

func compileMapPatterns(values map[string]string) (map[string]*regexp.Regexp, error) {
	if len(values) == 0 {
		return nil, nil
	}
	if len(values) > maxRuleMatcherValues {
		return nil, fmt.Errorf("too many entries")
	}
	result := make(map[string]*regexp.Regexp, len(values))
	for key, value := range values {
		key = strings.ToLower(strings.TrimSpace(key))
		if key == "" {
			return nil, fmt.Errorf("empty key")
		}
		patterns, err := compileGlobPatterns([]string{value})
		if err != nil {
			return nil, fmt.Errorf("%s: %w", key, err)
		}
		result[key] = patterns[0]
	}
	return result, nil
}

func ruleLayer(matcher RuleMatcher) (string, int) {
	switch {
	case len(matcher.AssetIDs) > 0 ||
		len(matcher.AssetNames) > 0 ||
		len(matcher.AssetAddresses) > 0:
		return "asset", 60
	case len(matcher.OrganizationIDs) > 0 ||
		len(matcher.NodeIDs) > 0 ||
		len(matcher.Labels) > 0 ||
		len(matcher.Attributes) > 0:
		return "business", 50
	case len(matcher.Adapters) > 0 ||
		len(matcher.PlatformFamilies) > 0 ||
		len(matcher.OSNames) > 0 ||
		len(matcher.OSIDs) > 0 ||
		len(matcher.VersionIDs) > 0 ||
		len(matcher.Kernels) > 0 ||
		len(matcher.Architectures) > 0 ||
		len(matcher.Shells) > 0 ||
		len(matcher.AvailableCommands) > 0 ||
		len(matcher.Capabilities) > 0:
		return "detected", 40
	case len(matcher.PlatformIDs) > 0 ||
		len(matcher.PlatformCategories) > 0 ||
		len(matcher.PlatformTypes) > 0 ||
		len(matcher.PlatformNames) > 0 ||
		len(matcher.BaseOS) > 0:
		return "platform", 30
	case len(matcher.Protocols) > 0 ||
		len(matcher.ConnectionMethods) > 0 ||
		len(matcher.Charsets) > 0 ||
		len(matcher.Databases) > 0:
		return "connection", 20
	default:
		return "global", 10
	}
}

func (r *ruleResolver) resolve(
	session SessionContext,
	profile AssetProfile,
	phase string,
) RuleResolution {
	resolution := RuleResolution{Phase: phase}
	prompts := make(map[string]struct{})
	for _, rule := range r.rules {
		if !rule.matcher.matches(session, profile) {
			continue
		}
		resolution.rules = append(resolution.rules, rule)
		resolution.Matches = append(resolution.Matches, rule.match())
		for _, prompt := range rule.definition.Enforce.PromptAppend {
			if _, exists := prompts[prompt]; exists {
				continue
			}
			prompts[prompt] = struct{}{}
			resolution.PromptInstructions = append(
				resolution.PromptInstructions, prompt,
			)
		}
	}
	return resolution
}

func (r *compiledRule) match() RuleMatch {
	return RuleMatch{
		ID: r.definition.ID, Source: r.source, Layer: r.layer,
		Priority: r.definition.Priority, Specificity: r.specificity,
		Reason: r.definition.Enforce.Reason,
	}
}

func (m compiledRuleMatcher) matches(
	session SessionContext,
	profile AssetProfile,
) bool {
	return matchValue(m.protocols, session.Protocol) &&
		matchValue(m.connectionMethods, session.ConnectionMethod) &&
		matchValue(m.assetIDs, session.AssetID) &&
		matchValue(m.assetNames, session.AssetName) &&
		matchValue(m.assetAddresses, session.AssetAddress) &&
		matchValue(m.organizationIDs, session.OrganizationID) &&
		matchInt(m.platformIDs, session.PlatformID) &&
		matchValue(m.platformCategories, session.PlatformCategory) &&
		matchValue(m.platformTypes, session.PlatformType) &&
		matchValue(m.platformNames, session.PlatformName) &&
		matchValue(m.baseOS, session.BaseOS) &&
		matchValue(m.charsets, session.Charset) &&
		matchValue(m.databases, session.Database) &&
		matchValues(m.nodeIDs, session.NodeIDs) &&
		matchMap(m.labels, session.Labels) &&
		matchMap(m.attributes, session.Attributes) &&
		matchValue(m.adapters, profile.Adapter) &&
		matchValue(m.platformFamilies, profile.PlatformFamily) &&
		matchValue(m.osNames, profile.OSName) &&
		matchValue(m.osIDs, profile.OSID) &&
		matchValue(m.versionIDs, profile.VersionID) &&
		matchValue(m.kernels, profile.Kernel) &&
		matchValue(m.architectures, profile.Architecture) &&
		matchValue(m.shells, profile.Shell) &&
		matchValues(m.availableCommands, profile.AvailableCommands) &&
		matchMap(m.capabilities, profile.Capabilities)
}

func (m compiledRuleMatcher) matchesCommand(command string) bool {
	if len(m.commandRegex) == 0 {
		return true
	}
	for _, pattern := range m.commandRegex {
		if pattern.MatchString(command) {
			return true
		}
	}
	return false
}

func matchValue(patterns []*regexp.Regexp, value string) bool {
	if len(patterns) == 0 {
		return true
	}
	if value == "" {
		return false
	}
	for _, pattern := range patterns {
		if pattern.MatchString(value) {
			return true
		}
	}
	return false
}

func matchValues(patterns []*regexp.Regexp, values []string) bool {
	if len(patterns) == 0 {
		return true
	}
	for _, value := range values {
		if matchValue(patterns, value) {
			return true
		}
	}
	return false
}

func matchInt(values map[int]struct{}, actual int) bool {
	if len(values) == 0 {
		return true
	}
	_, exists := values[actual]
	return exists
}

func matchMap(patterns map[string]*regexp.Regexp, values map[string]string) bool {
	for key, pattern := range patterns {
		value, exists := values[key]
		if !exists {
			for candidate, candidateValue := range values {
				if strings.EqualFold(candidate, key) {
					value, exists = candidateValue, true
					break
				}
			}
		}
		if !exists || !pattern.MatchString(value) {
			return false
		}
	}
	return true
}

func (r RuleResolution) commandPolicy(command string) RuleCommandPolicy {
	var policy RuleCommandPolicy
	for _, rule := range r.rules {
		if !rule.matcher.matchesCommand(command) {
			continue
		}
		enforcement := rule.definition.Enforce
		match := rule.match()
		policy.Matches = append(policy.Matches, match)
		if enforcement.MinimumRisk > policy.MinimumRisk {
			policy.MinimumRisk = enforcement.MinimumRisk
		}
		if enforcement.MinimumRisk > 0 {
			policy.RiskSources = append(policy.RiskSources, match)
		}
		policy.RequireApproval = policy.RequireApproval || enforcement.RequireApproval
		if enforcement.RequireApproval {
			policy.ApprovalSources = append(policy.ApprovalSources, match)
		}
		policy.Deny = policy.Deny || enforcement.Deny
		if enforcement.Deny {
			policy.DenySources = append(policy.DenySources, match)
		}
		policy.ForcePTY = policy.ForcePTY || enforcement.ForcePTY
		if enforcement.ForcePTY {
			policy.PTYSources = append(policy.PTYSources, match)
		}
		if enforcement.MaxExecutionSeconds > 0 &&
			(policy.MaxExecutionSeconds == 0 ||
				enforcement.MaxExecutionSeconds < policy.MaxExecutionSeconds) {
			policy.MaxExecutionSeconds = enforcement.MaxExecutionSeconds
		}
		if enforcement.MaxExecutionSeconds > 0 {
			policy.TimeoutSources = append(policy.TimeoutSources, match)
		}
	}
	return policy
}

func (r RuleResolution) disablesBackground() bool {
	for _, rule := range r.rules {
		if len(rule.matcher.commandRegex) == 0 && rule.definition.Enforce.ForcePTY {
			return true
		}
	}
	return false
}

func ruleCause(matches []RuleMatch) string {
	ids := make([]string, 0, len(matches))
	reasons := make([]string, 0, len(matches))
	seenReasons := make(map[string]struct{}, len(matches))
	for _, match := range matches {
		ids = append(ids, match.ID)
		if match.Reason != "" {
			if _, exists := seenReasons[match.Reason]; !exists {
				seenReasons[match.Reason] = struct{}{}
				reasons = append(reasons, match.Reason)
			}
		}
	}
	cause := "terminal AI rules " + strconv.Quote(strings.Join(ids, ", "))
	if len(reasons) > 0 {
		cause += ": " + strings.Join(reasons, "; ")
	}
	return cause
}
