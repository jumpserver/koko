package sessiontools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"
	"unicode"
)

const (
	MCPToolExecuteCommand    = "execute_command"
	MCPToolExecuteShell      = "execute_shell"
	MCPToolExecuteSQL        = "execute_sql"
	MCPToolExecuteRedis      = "execute_redis"
	MCPToolExecuteMongoDB    = "execute_mongodb"
	MCPExecutionModesMetaKey = "com.jumpserver/executionModes"
	MCPToolKindMetaKey       = "com.jumpserver/toolKind"
	MCPExecutionAuto         = "auto"
	MCPExecutionPTY          = "pty"
	MCPExecutionBackground   = "background"
	defaultMCPCommandTime    = 2 * time.Minute
	maximumMCPCommandTime    = 10 * time.Minute
	maximumMCPCommandSize    = 64 * 1024
)

type MCPCommandHooks struct {
	CommandACLCheck  func(string) CommandACLDecision
	CommandACLReview func(
		context.Context,
		CommandACLDecision,
		string,
	) (CommandACLDecision, error)
	BackgroundRecord    func(string, string, *int, *CommandACLDecision)
	ExecutionGuard      func() error
	BackgroundGuard     func() error
	BackgroundAvailable func() bool
	PTYExecute          func(
		context.Context,
		string,
		*CommandACLDecision,
	) (string, *int, error)
}

type CommandACLDecision struct {
	Action    string   `json:"action"`
	ACLID     string   `json:"acl_id,omitempty"`
	ItemID    string   `json:"item_id,omitempty"`
	Name      string   `json:"name,omitempty"`
	Matched   string   `json:"matched,omitempty"`
	DetailURL string   `json:"detail_url,omitempty"`
	Reviewers []string `json:"reviewers,omitempty"`
	Processor string   `json:"processor,omitempty"`
	Reviewed  bool     `json:"reviewed,omitempty"`
}

type CommandExecutor interface {
	Execute(context.Context, string, func(string)) (string, *int, error)
	Close() error
}

type CommandConstraints struct {
	BackgroundEligible  bool
	MaxExecutionSeconds int
}

type CommandValidator func(string) (CommandConstraints, error)

type MCPCommandToolOptions struct {
	Executor CommandExecutor
	Validate CommandValidator
	Protocol string
	Hooks    MCPCommandHooks
}

type MCPCommandResult struct {
	Command         string              `json:"command"`
	Execution       string              `json:"execution"`
	Output          string              `json:"output"`
	OutputTruncated bool                `json:"output_truncated,omitempty"`
	ExitCode        *int                `json:"exit_code,omitempty"`
	CommandACL      *CommandACLDecision `json:"command_acl,omitempty"`
}

type mcpCommandArguments struct {
	Command        string `json:"command"`
	Execution      string `json:"execution,omitempty"`
	TimeoutSeconds int    `json:"timeout_seconds,omitempty"`
}

type mcpCommandTool struct {
	executor CommandExecutor
	validate CommandValidator
	protocol string
	hooks    MCPCommandHooks
	close    sync.Once
}

func NewCommandTool(options MCPCommandToolOptions) (MCPToolHandler, error) {
	if options.Executor == nil && options.Hooks.PTYExecute == nil {
		return nil, errors.New("connection command executor is required")
	}
	if options.Validate == nil {
		return nil, errors.New("connection command validator is required")
	}
	return &mcpCommandTool{
		executor: options.Executor, validate: options.Validate,
		protocol: strings.ToLower(strings.TrimSpace(options.Protocol)), hooks: options.Hooks,
	}, nil
}

func (t *mcpCommandTool) Definition() MCPToolDefinition {
	executionModes := t.executionModes()
	title, description, commandDescription := commandToolPresentation(t.protocol)
	executionDescription := "Select how to execute the command; auto chooses the safest available mode"
	if isSQLProtocol(t.protocol) {
		executionDescription = "Use auto for SQL; PTY is only for a recognized session-dependent SQL statement"
	}
	return MCPToolDefinition{
		Name: commandToolName(t.protocol), Title: title, Description: description,
		OutputSchema: commandOutputSchema(),
		Annotations: map[string]any{
			"readOnlyHint": false, "openWorldHint": true,
		},
		Meta: map[string]any{
			MCPExecutionModesMetaKey: executionModes,
			MCPToolKindMetaKey:       "command",
		},
		InputSchema: map[string]any{
			"type": "object", "additionalProperties": false,
			"properties": map[string]any{
				"command": map[string]any{
					"type": "string", "minLength": 1,
					"maxLength":   maximumMCPCommandSize,
					"description": commandDescription,
				},
				"timeout_seconds": map[string]any{
					"type": "integer", "minimum": 1,
					"maximum":     int(maximumMCPCommandTime / time.Second),
					"description": "Execution timeout in seconds; omit to use the bounded session default",
				},
				"execution": map[string]any{
					"type": "string", "enum": executionModes,
					"description": executionDescription,
				},
			},
			"required": []string{"command"},
		},
	}
}

func (t *mcpCommandTool) executionModes() []string {
	executionModes := []string{MCPExecutionAuto}
	if t.hooks.PTYExecute != nil {
		executionModes = append(executionModes, MCPExecutionPTY)
	}
	if t.hooks.BackgroundAvailable != nil && t.hooks.BackgroundAvailable() {
		executionModes = append(executionModes, MCPExecutionBackground)
	}
	return executionModes
}

func (t *mcpCommandTool) Call(
	ctx context.Context,
	arguments json.RawMessage,
) (any, error) {
	var args mcpCommandArguments
	decoder := json.NewDecoder(bytes.NewReader(arguments))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&args); err != nil {
		return nil, fmt.Errorf("decode command arguments: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return nil, errors.New("command arguments have trailing data")
	}
	command := strings.TrimSpace(args.Command)
	if command == "" || len(command) > maximumMCPCommandSize ||
		containsCommandControl(command) {
		return nil, errors.New("invalid command")
	}
	constraints, err := t.validate(command)
	if err != nil {
		return nil, err
	}
	execution, err := t.selectExecution(args.Execution, constraints)
	if err != nil {
		return nil, err
	}
	if t.hooks.ExecutionGuard != nil {
		if err = t.hooks.ExecutionGuard(); err != nil {
			return nil, err
		}
	}
	decision, err := t.authorize(ctx, command)
	if err != nil {
		return nil, err
	}
	timeout := defaultMCPCommandTime
	if args.TimeoutSeconds > 0 {
		timeout = time.Duration(args.TimeoutSeconds) * time.Second
	}
	if timeout > maximumMCPCommandTime {
		return nil, fmt.Errorf(
			"timeout_seconds exceeds %d",
			int(maximumMCPCommandTime/time.Second),
		)
	}
	if constraints.MaxExecutionSeconds > 0 {
		ruleTimeout := time.Duration(constraints.MaxExecutionSeconds) * time.Second
		if ruleTimeout < timeout {
			timeout = ruleTimeout
		}
	}
	if t.hooks.ExecutionGuard != nil {
		if err = t.hooks.ExecutionGuard(); err != nil {
			return nil, err
		}
	}
	if err = t.recheckAuthorization(command, decision); err != nil {
		return nil, err
	}
	guard := t.hooks.ExecutionGuard
	if execution == MCPExecutionBackground {
		guard = t.hooks.BackgroundGuard
		if guard != nil {
			if err = guard(); err != nil {
				return nil, err
			}
		}
	}
	execCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	guardFailure := make(chan error, 1)
	guardDone := make(chan struct{})
	if guard != nil {
		go t.watchGuard(execCtx, cancel, guard, guardFailure, guardDone)
	}
	var output string
	var exitCode *int
	var executeErr error
	if execution == MCPExecutionPTY {
		output, exitCode, executeErr = t.hooks.PTYExecute(execCtx, command, decision)
	} else {
		if t.executor == nil {
			close(guardDone)
			return nil, errors.New("background command executor is unavailable")
		}
		output, exitCode, executeErr = t.executor.Execute(execCtx, command, nil)
	}
	close(guardDone)
	select {
	case guardErr := <-guardFailure:
		executeErr = guardErr
	default:
	}
	if executeErr == nil && guard != nil {
		if guardErr := guard(); guardErr != nil {
			executeErr = guardErr
		}
	}
	if execution == MCPExecutionBackground && t.hooks.BackgroundRecord != nil {
		recordedOutput := output
		if executeErr != nil {
			recordedOutput = strings.TrimSpace(recordedOutput + "\n" + executeErr.Error())
		}
		t.hooks.BackgroundRecord(command, recordedOutput, exitCode, decision)
	}
	if executeErr != nil {
		return nil, executeErr
	}
	return MCPCommandResult{
		Command: command, Execution: execution, Output: output,
		OutputTruncated: commandOutputTruncated(output),
		ExitCode:        exitCode, CommandACL: decision,
	}, nil
}

func containsCommandControl(command string) bool {
	for _, value := range command {
		if unicode.IsControl(value) {
			return true
		}
	}
	return false
}

func (t *mcpCommandTool) selectExecution(
	requested string,
	constraints CommandConstraints,
) (string, error) {
	requested = strings.ToLower(strings.TrimSpace(requested))
	if requested == "" {
		requested = MCPExecutionAuto
	}
	backgroundAvailable := t.hooks.BackgroundAvailable != nil &&
		t.hooks.BackgroundAvailable()
	switch requested {
	case MCPExecutionAuto:
		if constraints.BackgroundEligible && backgroundAvailable {
			return MCPExecutionBackground, nil
		}
		if t.hooks.PTYExecute != nil {
			return MCPExecutionPTY, nil
		}
		return "", errors.New("no eligible command execution path is available")
	case MCPExecutionBackground:
		if !constraints.BackgroundEligible {
			return "", errors.New("command is not eligible for background execution")
		}
		if !backgroundAvailable {
			return "", errors.New("background execution is unavailable")
		}
		return MCPExecutionBackground, nil
	case MCPExecutionPTY:
		if t.hooks.PTYExecute == nil {
			return "", errors.New("PTY execution is unavailable")
		}
		return MCPExecutionPTY, nil
	default:
		return "", errors.New("execution must be auto, pty, or background")
	}
}

func commandOutputTruncated(output string) bool {
	return strings.Contains(output, "output truncated")
}

func (t *mcpCommandTool) authorize(
	ctx context.Context,
	command string,
) (*CommandACLDecision, error) {
	if t.hooks.CommandACLCheck == nil {
		return nil, nil
	}
	decision := t.hooks.CommandACLCheck(command)
	switch decision.Action {
	case "reject":
		return nil, fmt.Errorf("command rejected by ACL %q", decision.Name)
	case "review":
		if t.hooks.CommandACLReview == nil {
			return nil, errors.New("command ACL review is unavailable")
		}
		reviewed, err := t.hooks.CommandACLReview(ctx, decision, command)
		if err != nil {
			return nil, err
		}
		if reviewed.Action != "accept" {
			return nil, errors.New("command rejected by ACL reviewer")
		}
		return &reviewed, nil
	case "", "Unknown":
		return nil, nil
	default:
		return &decision, nil
	}
}

func (t *mcpCommandTool) recheckAuthorization(
	command string,
	approved *CommandACLDecision,
) error {
	if t.hooks.CommandACLCheck == nil {
		return nil
	}
	current := t.hooks.CommandACLCheck(command)
	if current.Action == "reject" {
		return fmt.Errorf("command rejected by ACL %q", current.Name)
	}
	if approved == nil {
		if current.Action == "" || current.Action == "Unknown" {
			return nil
		}
		return errors.New("command ACL changed before execution")
	}
	if current.ACLID != approved.ACLID || current.ItemID != approved.ItemID {
		return errors.New("command ACL changed before execution")
	}
	if approved.Reviewed {
		if current.Action != "review" {
			return errors.New("reviewed command ACL changed before execution")
		}
		return nil
	}
	if current.Action != approved.Action {
		return errors.New("command ACL changed before execution")
	}
	return nil
}

func (t *mcpCommandTool) watchGuard(
	ctx context.Context,
	cancel context.CancelFunc,
	guard func() error,
	failure chan<- error,
	done <-chan struct{},
) {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-done:
			return
		case <-ticker.C:
			if err := guard(); err != nil {
				select {
				case failure <- err:
				default:
				}
				cancel()
				return
			}
		}
	}
}

func (t *mcpCommandTool) Close() error {
	var err error
	t.close.Do(func() {
		if t.executor != nil {
			err = t.executor.Close()
		}
	})
	return err
}
