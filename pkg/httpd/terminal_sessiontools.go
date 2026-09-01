package httpd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/jumpserver-dev/sdk-go/model"
	"github.com/jumpserver/koko/internal/agentapi"
	"github.com/jumpserver/koko/internal/sessiontools"
	"github.com/jumpserver/koko/pkg/logger"
	"github.com/jumpserver/koko/pkg/proxy"
	"github.com/jumpserver/koko/pkg/srvconn"
)

type commandExecutorFactory func(context.Context) (sessiontools.CommandExecutor, error)

type terminalToolController struct {
	client   *Client
	ws       *UserWebsocket
	server   *proxy.Server
	context  agentapi.ContextSnapshot
	protocol string

	ctx               context.Context
	cancel            context.CancelFunc
	mu                sync.Mutex
	closed            bool
	starting          bool
	resourceSessionID string
	factory           commandExecutorFactory
}

func newTerminalToolController(
	client *Client,
	ws *UserWebsocket,
	server *proxy.Server,
) (*terminalToolController, error) {
	if client == nil || ws == nil || server == nil || ws.ConnectToken == nil {
		return nil, fmt.Errorf("terminal tool controller dependencies are unavailable")
	}
	token := ws.ConnectToken
	pty := client.Pty()
	observer, err := sessiontools.NewTerminalObserver(
		int(pty.Window.Width), int(pty.Window.Height),
	)
	if err != nil {
		return nil, err
	}
	if !client.setTerminalObserver(observer) {
		_ = observer.Close()
		return nil, fmt.Errorf("terminal observer is already configured")
	}
	ctx, cancel := context.WithCancel(client.Context())
	controller := &terminalToolController{
		client: client, ws: ws, server: server,
		context: agentContextSnapshot(token, "terminal"), protocol: token.Protocol,
		ctx: ctx, cancel: cancel,
	}
	if !terminalBackgroundExecutorExpected(
		token.Protocol, server.SupportsBackgroundExecution(),
	) {
		controller.factory = func(context.Context) (sessiontools.CommandExecutor, error) {
			return nil, nil
		}
	}
	return controller, nil
}

func terminalBackgroundExecutorExpected(protocol string, backgroundEnabled bool) bool {
	if protocol == srvconn.ProtocolSSH {
		return true
	}
	return backgroundEnabled && sessiontools.ProtocolSupportsBackgroundExecutor(protocol)
}

func (c *terminalToolController) setResourceSession(info *proxy.SessionInfo) {
	if info == nil || info.Session == nil || info.Session.ID == "" {
		return
	}
	c.mu.Lock()
	if !c.closed {
		c.resourceSessionID = info.Session.ID
		c.startLocked()
	}
	c.mu.Unlock()
}

func (c *terminalToolController) attachSSH(client *srvconn.SSHClient) {
	if client == nil {
		return
	}
	c.attach(func(context.Context) (sessiontools.CommandExecutor, error) {
		return sessiontools.NewSSHExecutor(client), nil
	})
}

func (c *terminalToolController) attachDatabase(info proxy.DatabaseConnectionInfo) {
	config := sessiontools.DatabaseConfig{
		Protocol: info.Protocol,
		Host:     info.Host, Port: info.Port, ServerName: info.ServerName,
		Username: info.Username, Password: info.Password,
		Database: info.Database, UseSSL: info.UseSSL,
		PGSSLMode: info.PGSSLMode,
		CACert:    info.CACert, ClientCert: info.ClientCert, ClientKey: info.ClientKey,
		AllowInvalidCert: info.AllowInvalidCert,
		Encrypt:          info.Encrypt, DisableEncrypt: info.DisableEncrypt,
		ClusterMode: info.ClusterMode, AuthSource: info.AuthSource,
		ConnectionOpts: info.ConnectionOpts, ProxyURL: info.ProxyURL,
		DataMaskingRules: append(
			[]model.DataMaskingRule(nil), info.DataMaskingRules...,
		),
	}
	c.attach(func(ctx context.Context) (sessiontools.CommandExecutor, error) {
		return sessiontools.NewDatabaseExecutor(ctx, config)
	})
}

func (c *terminalToolController) attach(factory commandExecutorFactory) {
	c.mu.Lock()
	if !c.closed && c.factory == nil {
		c.factory = factory
		c.startLocked()
	}
	c.mu.Unlock()
}

func (c *terminalToolController) startLocked() {
	if c.closed || c.starting || c.resourceSessionID == "" || c.factory == nil {
		return
	}
	c.starting = true
	resourceID := c.resourceSessionID
	factory := c.factory
	go c.initialize(resourceID, factory)
}

func (c *terminalToolController) initialize(
	resourceID string,
	factory commandExecutorFactory,
) {
	ctx, cancel := context.WithTimeout(c.ctx, 30*time.Second)
	defer cancel()
	executor, err := factory(ctx)
	if err != nil {
		logger.Errorf(
			"Terminal %d command executor unavailable (%s)",
			c.client.TerminalId, commandExecutorErrorClass(err),
		)
		executor = nil
	}
	commandTool, err := sessiontools.NewCommandTool(sessiontools.MCPCommandToolOptions{
		Executor: executor, Protocol: c.protocol,
		Validate: sessiontools.ProtocolCommandValidator(c.protocol),
		Hooks: sessiontools.MCPCommandHooks{
			CommandACLCheck: func(command string) sessiontools.CommandACLDecision {
				return agentToolACLDecision(c.server.MatchCommandACL(command))
			},
			CommandACLReview: c.reviewCommand,
			BackgroundRecord: c.recordCommand,
			ExecutionGuard:   c.server.CheckAgentToolExecution,
			BackgroundGuard:  c.server.CheckBackgroundExecution,
			BackgroundAvailable: func() bool {
				return executor != nil && c.server.SupportsBackgroundExecution()
			},
			PTYExecute: c.executePTY,
		},
	})
	if err != nil {
		if executor != nil {
			_ = executor.Close()
		}
		logger.Errorf("Terminal %d command tool unavailable: %s", c.client.TerminalId, err)
		return
	}
	snapshotTool, _ := sessiontools.NewTerminalSnapshotTool(func() (any, error) {
		if err := c.server.CheckAgentToolExecution(); err != nil {
			return nil, err
		}
		observer := c.client.getTerminalObserver()
		if observer == nil {
			return nil, fmt.Errorf("terminal observer is unavailable")
		}
		return map[string]any{
			"content": observer.Snapshot(), "max_bytes": 64 * 1024,
		}, nil
	})
	// The immutable resource context is already part of the manifest and model
	// request, so exposing the same value as a tool only invites a redundant call.
	handlers := []sessiontools.MCPToolHandler{snapshotTool, commandTool}
	if metadata, ok := executor.(sessiontools.SQLMetadataProvider); ok {
		if schemaTool, schemaErr := sessiontools.NewDatabaseSchemaTool(
			metadata, c.server.CheckBackgroundExecution,
		); schemaErr == nil {
			handlers = append(handlers, schemaTool)
		}
	}
	dispatcher, err := sessiontools.NewMCPDispatcher(
		c.ctx,
		sessiontools.MCPDispatcherOptions{
			ResourceSessionID: resourceID, Profile: "terminal",
			Context: c.context, Handlers: handlers,
			Emit: func(outbound sessiontools.MCPOutbound) {
				c.ws.SendMessage(&Message{
					Id: c.ws.Uuid, Type: outbound.Type,
					Version:           sessiontools.MCPProtocolVersion,
					ResourceSessionID: resourceID,
					TerminalId:        c.client.TerminalId,
					Data:              string(outbound.Data),
				})
			},
		},
	)
	if err != nil {
		if executor != nil {
			_ = executor.Close()
		}
		logger.Errorf("Terminal %d MCP dispatcher unavailable: %s", c.client.TerminalId, err)
		return
	}
	c.mu.Lock()
	installed := !c.closed && c.resourceSessionID == resourceID &&
		c.client.setMCP(dispatcher)
	c.mu.Unlock()
	if !installed {
		dispatcher.Close()
		return
	}
	if err = dispatcher.AnnounceManifest(); err != nil {
		logger.Errorf("Terminal %d MCP manifest failed: %s", c.client.TerminalId, err)
	}
}

func commandExecutorErrorClass(err error) string {
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	case errors.Is(err, context.Canceled):
		return "cancelled"
	default:
		return "initialization_failed"
	}
}

func (c *terminalToolController) reviewCommand(
	ctx context.Context,
	decision sessiontools.CommandACLDecision,
	command string,
) (sessiontools.CommandACLDecision, error) {
	reviewed, err := c.server.ReviewCommand(ctx, proxy.CommandACLDecision{
		Action: model.CommandAction(decision.Action),
		ACLID:  decision.ACLID, ItemID: decision.ItemID,
		Name: decision.Name, Matched: decision.Matched,
		Reviewed: decision.Reviewed,
	}, command)
	return agentToolACLDecision(reviewed), err
}

func (c *terminalToolController) recordCommand(
	command, output string,
	exitCode *int,
	decision *sessiontools.CommandACLDecision,
) {
	var value *proxy.CommandACLDecision
	if decision != nil {
		value = &proxy.CommandACLDecision{
			Action: model.CommandAction(decision.Action),
			ACLID:  decision.ACLID, ItemID: decision.ItemID,
			Name: decision.Name, Matched: decision.Matched,
			Reviewed: decision.Reviewed,
		}
	}
	c.server.RecordBackgroundCommand(command, output, exitCode, value)
}

func (c *terminalToolController) executePTY(
	ctx context.Context,
	command string,
	decision *sessiontools.CommandACLDecision,
) (string, *int, error) {
	if err := c.server.CheckAgentToolExecution(); err != nil {
		return "", nil, err
	}
	if err := c.recheckPTYACL(command, decision); err != nil {
		return "", nil, err
	}
	observer := c.client.getTerminalObserver()
	if observer == nil {
		return "", nil, fmt.Errorf("terminal observer is unavailable")
	}
	result, err := observer.Begin(command)
	if err != nil {
		return "", nil, err
	}
	c.client.SetInputLocked(true)
	defer c.client.SetInputLocked(false)
	if decision != nil {
		value := proxy.CommandACLDecision{
			Action: model.CommandAction(decision.Action),
			ACLID:  decision.ACLID, ItemID: decision.ItemID,
			Name: decision.Name, Matched: decision.Matched,
			Reviewed: decision.Reviewed,
		}
		c.server.AuthorizeAgentToolCommand(command, &value)
		defer c.server.RevokeAgentToolCommand(command)
	}
	if err = c.client.WriteAgentToolData([]byte(command + "\r")); err != nil {
		observer.Cancel()
		return "", nil, err
	}
	select {
	case <-ctx.Done():
		_ = c.client.WriteAgentToolData([]byte{3})
		output := observer.Snapshot()
		observer.Cancel()
		return output, nil, ctx.Err()
	case observed := <-result:
		if observed.Command != command {
			return observed.Output, nil, fmt.Errorf("terminal observer command mismatch")
		}
		return observed.Output, nil, nil
	}
}

func (c *terminalToolController) recheckPTYACL(
	command string,
	approved *sessiontools.CommandACLDecision,
) error {
	current := agentToolACLDecision(c.server.MatchCommandACL(command))
	if current.Action == string(model.ActionReject) {
		return fmt.Errorf("command rejected by ACL %q", current.Name)
	}
	if approved == nil {
		if current.Action == "" || current.Action == string(model.ActionUnknown) {
			return nil
		}
		return fmt.Errorf("command ACL changed before PTY execution")
	}
	if current.ACLID != approved.ACLID || current.ItemID != approved.ItemID {
		return fmt.Errorf("command ACL changed before PTY execution")
	}
	if approved.Reviewed {
		if current.Action != string(model.ActionReview) {
			return fmt.Errorf("reviewed command ACL changed before PTY execution")
		}
		return nil
	}
	if current.Action != approved.Action {
		return fmt.Errorf("command ACL changed before PTY execution")
	}
	return nil
}

func (c *terminalToolController) handle(message *Message) {
	if message.Version != sessiontools.MCPProtocolVersion {
		sendMCPFrameError(c.ws, message, fmt.Errorf("unsupported MCP frame version"))
		return
	}
	dispatcher := c.client.getMCP()
	if dispatcher == nil || message.ResourceSessionID != c.resourceSessionID {
		sendMCPFrameError(c.ws, message, fmt.Errorf("MCP resource is unavailable"))
		return
	}
	var err error
	if message.Type == MCPRequest {
		err = dispatcher.HandleRequest([]byte(message.Data))
	} else {
		err = dispatcher.HandleCancel([]byte(message.Data))
	}
	if err != nil {
		sendMCPFrameError(c.ws, message, err)
	}
}

func (c *terminalToolController) Close() {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return
	}
	c.closed = true
	c.cancel()
	c.mu.Unlock()
	c.client.closeMCP()
	c.client.closeTerminalObserver()
}

func agentToolACLDecision(value proxy.CommandACLDecision) sessiontools.CommandACLDecision {
	return sessiontools.CommandACLDecision{
		Action: string(value.Action), ACLID: value.ACLID,
		ItemID: value.ItemID, Name: value.Name, Matched: value.Matched,
		DetailURL: value.DetailURL, Reviewers: value.Reviewers,
		Processor: value.Processor, Reviewed: value.Reviewed,
	}
}

func marshalTerminalSessionInfo(info *proxy.SessionInfo) string {
	data, _ := json.Marshal(info)
	return string(data)
}
