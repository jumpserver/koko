package terminalai

import (
	"context"
	"fmt"

	"github.com/jumpserver/koko/pkg/terminalai/provider"
)

var (
	_ Feature = builtInFeature{}
	_ Session = (*builtInSession)(nil)
)

type builtInFeature struct{}

func (builtInFeature) Configure(
	ctx context.Context,
	configuration Configuration,
) (ConfigurationResult, error) {
	var (
		count int
		err   error
	)
	switch {
	case configuration.RuleProvider != nil:
		count, err = ConfigureRuleProvider(ctx, configuration.RuleProvider)
	default:
		count, err = ConfigureRulesFile(ctx, configuration.RulesFile)
	}
	return ConfigurationResult{RuleCount: count}, err
}

func (builtInFeature) NewSession(options SessionOptions) (Session, error) {
	if options.WritePTY == nil {
		return nil, fmt.Errorf("terminal AI PTY writer is required")
	}
	if options.Emit == nil {
		return nil, fmt.Errorf("terminal AI message emitter is required")
	}
	config := options.Config
	journal := newAuditWriter(
		options.UserID,
		options.TerminalID,
		config.MemoryRoot,
		config.MemorySessions,
	)
	config.Provider.Trace = journal
	modelClient, err := NewModelClient(config)
	if err != nil {
		journal.Close()
		return nil, err
	}
	modelClient.SetResponseLanguage(options.Language)
	observer, err := NewObserver(options.Width, options.Height)
	if err != nil {
		journal.Close()
		return nil, err
	}
	runtime := NewRuntime(
		options.TerminalID,
		modelClient,
		observer,
		options.WritePTY,
		options.Emit,
	)
	runtime.SetAdapter(ResolveAdapter(options.Context))
	runtime.SetAuditWriter(journal)
	runtime.SetModelLimits(config.MaxModelRequests, config.Provider.RequestTimeout)
	if options.RequireCommandACL {
		runtime.RequireCommandACL()
	}
	if options.SetInputLocked != nil {
		runtime.SetInputLock(options.SetInputLocked)
	}
	return &builtInSession{
		runtime:  runtime,
		observer: observer,
		context:  normalizeSessionContext(options.Context),
		info:     modelClient.ProviderInfo(),
	}, nil
}

func (builtInFeature) SupportsBackground(sessionContext SessionContext) bool {
	return supportsBackground(sessionContext)
}

type builtInSession struct {
	runtime  *Runtime
	observer *Observer
	context  SessionContext
	info     provider.ProviderInfo
}

func (s *builtInSession) Handle(message ChatMessage) error {
	return s.runtime.Handle(message)
}

func (s *builtInSession) Feed(data []byte) {
	s.observer.Feed(data)
}

func (s *builtInSession) Resize(width, height int) {
	s.observer.Resize(width, height)
}

func (s *builtInSession) Bind(hooks SessionHooks) {
	s.runtime.SetCommandACL(hooks.CommandACLCheck, hooks.CommandACLReview)
	s.runtime.SetBackgroundRecorder(hooks.BackgroundRecord)
	s.runtime.SetBackgroundGuard(hooks.BackgroundGuard)
	s.runtime.SetPTYAuthorizer(hooks.PTYAuthorizer)
}

func (s *builtInSession) AttachBackground(
	ctx context.Context,
	connection BackgroundConnection,
) error {
	executor, profileProvider, registered, err := resolveBackgroundExecutor(
		ctx,
		s.context,
		connection,
	)
	if !registered {
		return nil
	}
	if err != nil {
		s.runtime.DisableBackground(err.Error())
		return err
	}
	s.runtime.SetBackgroundExecutor(executor, profileProvider)
	return nil
}

func (s *builtInSession) SetSessionID(sessionID string) {
	s.runtime.SetSessionID(sessionID)
}

func (s *builtInSession) DisableBackground(reason string) {
	s.runtime.DisableBackground(reason)
}

func (s *builtInSession) AnnounceCapability() {
	s.runtime.AnnounceCapability()
}

func (s *builtInSession) ProviderInfo() provider.ProviderInfo {
	return s.info
}

func (s *builtInSession) Close() {
	s.runtime.Close()
	_ = s.observer.Close()
}
