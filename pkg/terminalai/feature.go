package terminalai

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/jumpserver-dev/sdk-go/model"
	"github.com/jumpserver/koko/pkg/srvconn"
	"github.com/jumpserver/koko/pkg/terminalai/provider"
)

const builtInFeatureName = "builtin"

// Configuration contains process-level Terminal AI configuration.
type Configuration struct {
	RulesFile    string
	RuleProvider RuleProvider
}

type ConfigurationResult struct {
	RuleCount int
}

// SessionOptions contains immutable dependencies for one terminal session.
type SessionOptions struct {
	TerminalID        uint32
	UserID            string
	Width             int
	Height            int
	Config            Config
	Context           SessionContext
	Language          string
	WritePTY          func([]byte)
	Emit              func(ChatMessage)
	SetInputLocked    func(bool)
	RequireCommandACL bool
}

// SessionHooks binds terminal authorization and recording to an AI session.
type SessionHooks struct {
	CommandACLCheck  func(string) CommandACLDecision
	CommandACLReview func(
		context.Context,
		CommandACLDecision,
		string,
	) (CommandACLDecision, error)
	BackgroundRecord func(
		string,
		string,
		*int,
		*CommandACLDecision,
	)
	BackgroundGuard func() error
	PTYAuthorizer   func(string, *CommandACLDecision)
}

// DatabaseConfig is the connection material supplied to a background executor.
type DatabaseConfig struct {
	Protocol         string
	Host             string
	Port             int
	ServerName       string
	Username         string
	Password         string
	Database         string
	UseSSL           bool
	CACert           string
	ClientCert       string
	ClientKey        string
	AllowInvalidCert bool
	DataMaskingRules []model.DataMaskingRule
}

type BackgroundConnection struct {
	SSHClient *srvconn.SSHClient
	Database  *DatabaseConfig
}

// Session is the complete lifecycle boundary used by terminal transports.
type Session interface {
	Handle(ChatMessage) error
	Feed([]byte)
	Resize(width, height int)
	Bind(SessionHooks)
	AttachBackground(context.Context, BackgroundConnection) error
	SetSessionID(string)
	DisableBackground(string)
	AnnounceCapability()
	ProviderInfo() provider.ProviderInfo
	Close()
}

// Feature is the implementation boundary between Koko and Terminal AI.
type Feature interface {
	Configure(context.Context, Configuration) (ConfigurationResult, error)
	NewSession(SessionOptions) (Session, error)
	SupportsBackground(SessionContext) bool
}

var featureRegistry = struct {
	sync.RWMutex
	name        string
	feature     Feature
	registered  bool
	initialized bool
}{
	name:    builtInFeatureName,
	feature: builtInFeature{},
}

// RegisterFeature replaces the built-in implementation for this process.
// Registration must happen during process initialization before Koko starts.
func RegisterFeature(name string, feature Feature) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("terminal AI feature name is required")
	}
	if feature == nil {
		return fmt.Errorf("terminal AI feature %s is nil", name)
	}
	featureRegistry.Lock()
	defer featureRegistry.Unlock()
	if featureRegistry.initialized {
		return fmt.Errorf(
			"terminal AI feature %s is already initialized",
			featureRegistry.name,
		)
	}
	if featureRegistry.registered {
		return fmt.Errorf(
			"terminal AI feature %s is already registered",
			featureRegistry.name,
		)
	}
	featureRegistry.name = name
	featureRegistry.feature = feature
	featureRegistry.registered = true
	return nil
}

func FeatureName() string {
	featureRegistry.RLock()
	name := featureRegistry.name
	featureRegistry.RUnlock()
	return name
}

func Configure(
	ctx context.Context,
	configuration Configuration,
) (ConfigurationResult, error) {
	return activeFeature().Configure(ctx, configuration)
}

func NewSession(options SessionOptions) (Session, error) {
	return activeFeature().NewSession(options)
}

func SupportsBackground(sessionContext SessionContext) bool {
	return activeFeature().SupportsBackground(sessionContext)
}

func activeFeature() Feature {
	featureRegistry.Lock()
	featureRegistry.initialized = true
	feature := featureRegistry.feature
	featureRegistry.Unlock()
	return feature
}
