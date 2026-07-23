package terminalai

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jumpserver/koko/pkg/config"
)

const (
	defaultApprovalThreshold = 2
	defaultExecutionTimeout  = 5 * time.Minute
	profileTimeout           = 30 * time.Second
)

type pendingApproval struct {
	id       string
	digest   string
	proposal CommandProposal
	decision chan approvalDecision
}

type Runtime struct {
	terminalID     uint32
	model          LoopModel
	observer       *Observer
	writePTY       func([]byte)
	emit           func(ChatMessage)
	lifetimeCtx    context.Context
	lifetimeCancel context.CancelFunc

	mu                sync.Mutex
	wg                sync.WaitGroup
	busy              bool
	cancel            context.CancelFunc
	pending           *pendingApproval
	approvalThreshold int
	executionMode     string
	history           []string
	closed            bool
	activeExecution   string
	aclCheck          func(string) CommandACLDecision
	aclReview         func(context.Context, CommandACLDecision, string) (CommandACLDecision, error)
	aclRequired       bool
	backgroundRecord  func(string, string, *int, *CommandACLDecision)
	backgroundGuard   func() error
	authorizePTY      func(string, *CommandACLDecision)
	inputLock         func(bool)
	adapter           Adapter

	executorMu          sync.RWMutex
	backgroundExecutor  BackgroundExecutor
	backgroundAvailable bool
	backgroundReason    string
	profile             AssetProfile
	profileReady        chan struct{}
	profileOnce         sync.Once
	profileReadyOnce    sync.Once
	profileWG           sync.WaitGroup
	aclReady            chan struct{}
	aclReadyOnce        sync.Once

	audit        *auditWriter
	auditPending []auditEvent
}

func NewRuntime(
	terminalID uint32,
	model LoopModel,
	observer *Observer,
	writePTY func([]byte),
	emit func(ChatMessage),
) *Runtime {
	lifetimeCtx, lifetimeCancel := context.WithCancel(context.Background())
	return &Runtime{
		terminalID: terminalID, model: model, observer: observer,
		writePTY: writePTY, emit: emit,
		lifetimeCtx: lifetimeCtx, lifetimeCancel: lifetimeCancel,
		approvalThreshold: defaultApprovalThreshold,
		executionMode:     ModeAuto,
		profileReady:      make(chan struct{}),
		aclReady:          make(chan struct{}),
	}
}

func (r *Runtime) SetSessionID(sessionID string) {
	r.mu.Lock()
	if r.audit == nil {
		r.audit = newAuditWriter(sessionID, r.terminalID)
	}
	writer := r.audit
	pending := r.auditPending
	r.auditPending = nil
	r.mu.Unlock()
	for _, event := range pending {
		writer.Write(event.name, event.payload)
	}
}

func (r *Runtime) AnnounceCapability() {
	r.emitCapability()
}

func (r *Runtime) DisableBackground(reason string) {
	r.executorMu.Lock()
	r.backgroundAvailable = false
	r.backgroundReason = reason
	r.executorMu.Unlock()
	r.mu.Lock()
	r.executionMode = ModePTYOnly
	r.mu.Unlock()
	r.finishProfile(AssetProfile{DetectionError: reason})
	r.emitCapability()
	r.writeAudit("background_disabled", map[string]any{"reason": reason})
}

func (r *Runtime) SetAdapter(adapter Adapter) {
	if adapter == nil {
		return
	}
	r.mu.Lock()
	r.adapter = adapter
	r.profile = adapter.Profile()
	r.mu.Unlock()
	r.executorMu.Lock()
	if adapter.SupportsBackground() {
		r.backgroundReason = "background executor is initializing"
	} else {
		r.backgroundReason = "this asset adapter provides PTY execution only"
	}
	r.executorMu.Unlock()
	if adapter.Name() != "ssh-shell" {
		r.finishProfile(AssetProfile{})
	}
}

func (r *Runtime) SetCommandACL(
	check func(string) CommandACLDecision,
	review func(context.Context, CommandACLDecision, string) (CommandACLDecision, error),
) {
	r.mu.Lock()
	r.aclCheck = check
	r.aclReview = review
	r.mu.Unlock()
	r.aclReadyOnce.Do(func() { close(r.aclReady) })
}

func (r *Runtime) RequireCommandACL() {
	r.mu.Lock()
	r.aclRequired = true
	r.mu.Unlock()
}

func (r *Runtime) SetBackgroundRecorder(
	record func(string, string, *int, *CommandACLDecision),
) {
	r.mu.Lock()
	r.backgroundRecord = record
	r.mu.Unlock()
}

func (r *Runtime) SetBackgroundGuard(check func() error) {
	r.mu.Lock()
	r.backgroundGuard = check
	r.mu.Unlock()
}

func (r *Runtime) SetInputLock(lock func(bool)) {
	r.mu.Lock()
	r.inputLock = lock
	r.mu.Unlock()
}

func (r *Runtime) SetPTYAuthorizer(
	authorize func(string, *CommandACLDecision),
) {
	r.mu.Lock()
	r.authorizePTY = authorize
	r.mu.Unlock()
}

func (r *Runtime) SetBackgroundExecutor(
	executor BackgroundExecutor, profileProvider ProfileProvider,
) {
	if executor == nil {
		return
	}
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		_ = executor.Close()
		return
	}
	adapter := r.adapter
	if adapter == nil || !adapter.SupportsBackground() {
		r.mu.Unlock()
		_ = executor.Close()
		return
	}
	r.executorMu.Lock()
	previous := r.backgroundExecutor
	r.backgroundExecutor = executor
	r.backgroundAvailable = true
	r.backgroundReason = ""
	r.executorMu.Unlock()
	if profileProvider != nil {
		r.profileOnce.Do(func() {
			r.profileWG.Add(1)
			go func() {
				defer r.profileWG.Done()
				ctx, cancel := context.WithTimeout(r.lifetimeCtx, profileTimeout)
				defer cancel()
				r.finishProfile(profileProvider.DetectProfile(ctx))
			}()
		})
	}
	r.mu.Unlock()
	if previous != nil {
		_ = previous.Close()
	}
	r.emitCapability()
}

func (r *Runtime) finishProfile(profile AssetProfile) {
	r.mu.Lock()
	if profile.Adapter == "" {
		profile.Adapter = r.profile.Adapter
	}
	if profile.CommandLanguage == "" {
		profile.CommandLanguage = r.profile.CommandLanguage
	}
	if profile.SessionContext.Protocol == "" {
		profile.SessionContext = r.profile.SessionContext
	}
	r.profile = profile
	r.mu.Unlock()
	r.profileReadyOnce.Do(func() { close(r.profileReady) })
}

func (r *Runtime) Handle(message ChatMessage) error {
	if message.Role != "user" {
		return fmt.Errorf("only user chat messages are accepted")
	}
	for _, part := range message.Parts {
		switch part.Type {
		case "text":
			question := strings.TrimSpace(part.Text)
			if question == "" {
				continue
			}
			return r.start(question)
		case "data-approval":
			var decision approvalDecision
			if err := decodePartData(part.Data, &decision); err != nil {
				return err
			}
			return r.resolveApproval(decision)
		case "data-policy":
			var policy policyUpdate
			if err := decodePartData(part.Data, &policy); err != nil {
				return err
			}
			return r.updatePolicy(policy)
		case "data-interrupt":
			r.Interrupt()
			return nil
		}
	}
	return fmt.Errorf("chat message has no supported part")
}

func (r *Runtime) start(question string) error {
	if len(question) > 32*1024 {
		return fmt.Errorf("terminal AI message is too large")
	}
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return fmt.Errorf("terminal AI is closed")
	}
	if r.busy {
		r.mu.Unlock()
		return fmt.Errorf("another terminal AI task is active")
	}
	ctx, cancel := context.WithCancel(r.lifetimeCtx)
	r.busy = true
	r.cancel = cancel
	r.history = append(r.history, "user: "+question)
	r.trimHistoryLocked()
	r.wg.Add(1)
	r.mu.Unlock()
	r.writeAudit("user_message", map[string]any{"text": question})
	go func() {
		defer r.wg.Done()
		r.run(ctx, question)
	}()
	return nil
}

func (r *Runtime) run(ctx context.Context, question string) {
	defer func() {
		r.mu.Lock()
		r.busy = false
		r.cancel = nil
		r.pending = nil
		r.activeExecution = ""
		r.mu.Unlock()
		r.emitProgress("", "idle", false)
	}()
	r.emitProgress("正在读取当前终端状态…", "tool_running", true)
	profile := r.currentProfile(ctx)
	snapshot := r.observer.Snapshot()
	r.mu.Lock()
	history := strings.Join(r.history, "\n")
	mode := r.executionMode
	r.mu.Unlock()
	r.emitProgress("正在分析请求…", "analyzing", true)
	var decision Decision
	if err := r.retry(ctx, func(callCtx context.Context) error {
		var err error
		decision, err = r.model.Decide(callCtx, question, history, profile.String(), snapshot)
		return err
	}); err != nil {
		r.emitError(err)
		return
	}
	if decision.Kind == "answer" {
		r.emitText(decision.Answer, "final")
		r.appendAssistantHistory(decision.Answer)
		return
	}
	if decision.Kind != "plan" || len(decision.Steps) == 0 || len(decision.Steps) > 20 {
		r.emitError(fmt.Errorf("model returned an invalid plan"))
		return
	}
	planID := runtimeID("plan")
	for index := range decision.Steps {
		decision.Steps[index].ID = fmt.Sprintf("%s-step-%d", planID, index+1)
		decision.Steps[index].Status = "pending"
	}
	r.emitData("data-plan", map[string]any{
		"id": planID, "summary": decision.Summary, "steps": decision.Steps,
	}, "process")
	results := make([]StepResult, 0, len(decision.Steps))
	for index := range decision.Steps {
		if ctx.Err() != nil {
			return
		}
		r.emitProgress(
			fmt.Sprintf("正在准备第 %d/%d 步…", index+1, len(decision.Steps)),
			"planning", true,
		)
		r.executorMu.RLock()
		backgroundAvailable := r.backgroundAvailable
		r.executorMu.RUnlock()
		snapshot = r.observer.Snapshot()
		var proposal CommandProposal
		if err := r.retry(ctx, func(callCtx context.Context) error {
			var err error
			proposal, err = r.model.Propose(
				callCtx, question, decision.Summary, decision.Steps, index,
				profile.String(), snapshot, results, mode, backgroundAvailable,
			)
			return err
		}); err != nil {
			r.emitError(err)
			return
		}
		if err := validateProposal(&proposal); err != nil {
			r.emitError(err)
			return
		}
		r.mu.Lock()
		adapter := r.adapter
		r.mu.Unlock()
		if adapter == nil {
			r.emitError(fmt.Errorf("terminal AI adapter is unavailable"))
			return
		}
		if err := adapter.PrepareProposal(&proposal); err != nil {
			r.emitError(err)
			return
		}
		if err := r.applyExecutionMode(&proposal); err != nil {
			r.emitError(err)
			return
		}
		approvedProposal, err := r.authorize(ctx, planID, decision.Steps[index], index, len(decision.Steps), proposal)
		if err != nil {
			if !errors.Is(err, context.Canceled) {
				r.emitError(err)
			}
			return
		}
		proposal = approvedProposal
		r.emitProgress("正在执行命令…", "executing", true)
		r.mu.Lock()
		r.activeExecution = proposal.Execution
		r.mu.Unlock()
		output, exitCode, err := r.execute(ctx, proposal, func(output string) {
			r.emitData("data-execution", map[string]any{
				"planId": planID, "stepId": decision.Steps[index].ID,
				"step": index + 1, "total": len(decision.Steps),
				"command": proposal.Command, "execution": proposal.Execution,
				"output": output, "outcome": "running",
			}, "process")
		})
		r.mu.Lock()
		r.activeExecution = ""
		backgroundRecord := r.backgroundRecord
		r.mu.Unlock()
		if proposal.Execution == ExecutionBackground && backgroundRecord != nil {
			if err != nil {
				output = strings.TrimSpace(output + "\n" + err.Error())
			}
			backgroundRecord(proposal.Command, output, exitCode, proposal.CommandACL)
		} else if err != nil {
			output = strings.TrimSpace(output + "\n" + err.Error())
		}
		r.emitData("data-execution", map[string]any{
			"planId": planID, "stepId": decision.Steps[index].ID,
			"step": index + 1, "total": len(decision.Steps),
			"command": proposal.Command, "execution": proposal.Execution,
			"output": output, "exitCode": exitCode, "outcome": "reviewing",
		}, "process")
		var review StepReview
		if reviewErr := r.retry(ctx, func(callCtx context.Context) error {
			var callErr error
			review, callErr = r.model.Review(
				callCtx, decision.Steps[index], proposal, output, exitCode,
			)
			return callErr
		}); reviewErr != nil {
			r.emitError(reviewErr)
			return
		}
		status := "completed"
		if review.Outcome != "completed" {
			status = "failed"
		}
		decision.Steps[index].Status = status
		result := StepResult{
			StepID: decision.Steps[index].ID, Command: proposal.Command,
			Output: output, Status: status, Summary: review.Summary,
			Execution: proposal.Execution, ExitCode: exitCode,
		}
		results = append(results, result)
		r.emitData("data-execution", map[string]any{
			"planId": planID, "stepId": result.StepID,
			"step": index + 1, "total": len(decision.Steps),
			"command": result.Command, "execution": result.Execution,
			"output": result.Output, "exitCode": result.ExitCode,
			"outcome": status, "summary": result.Summary,
		}, "process")
		r.emitData("data-plan", map[string]any{
			"id": planID, "summary": decision.Summary, "steps": decision.Steps,
		}, "process")
		if status == "failed" {
			break
		}
	}
	r.emitProgress("正在生成执行总结…", "summarizing", true)
	var summary string
	if err := r.retry(ctx, func(callCtx context.Context) error {
		var callErr error
		summary, callErr = r.model.Summarize(
			callCtx, question, decision.Summary, decision.Steps, results,
		)
		return callErr
	}); err != nil {
		r.emitError(err)
		return
	}
	r.emitText(summary, "final")
	r.appendAssistantHistory(summary)
}

func (r *Runtime) authorize(
	ctx context.Context,
	planID string,
	step Step,
	index, total int,
	proposal CommandProposal,
) (CommandProposal, error) {
	r.mu.Lock()
	aclRequired := r.aclRequired
	r.mu.Unlock()
	if aclRequired {
		select {
		case <-ctx.Done():
			return CommandProposal{}, ctx.Err()
		case <-r.aclReady:
		}
	}
	r.mu.Lock()
	threshold := r.approvalThreshold
	mode := r.executionMode
	aclCheck := r.aclCheck
	aclReview := r.aclReview
	r.mu.Unlock()
	forceApproval := false
	var aclDecision CommandACLDecision
	if aclCheck != nil {
		aclDecision = aclCheck(proposal.Command)
		switch aclDecision.Action {
		case "reject":
			r.emitData("data-command-acl", aclDecision, "final")
			r.writeAudit("command_acl_rejected", aclDecision)
			return CommandProposal{}, fmt.Errorf("command rejected by ACL %q", aclDecision.Name)
		case "review":
			proposal.RiskLevel, proposal.RiskReason = raiseRisk(
				proposal.RiskLevel, proposal.RiskReason, 3,
				"command requires approval by the existing command ACL",
			)
			r.emitData("data-command-acl", map[string]any{
				"state": "waiting_for_review", "command": proposal.Command,
				"decision": aclDecision,
			}, "process")
			if aclReview == nil {
				return CommandProposal{}, fmt.Errorf("command ACL review is unavailable")
			}
			reviewed, err := aclReview(ctx, aclDecision, proposal.Command)
			if err != nil {
				return CommandProposal{}, err
			}
			aclDecision = reviewed
			state := "approved"
			if reviewed.Action != "accept" {
				state = "rejected"
			}
			r.emitData("data-command-acl", map[string]any{
				"state": state, "command": proposal.Command,
				"decision": reviewed,
			}, "process")
			if reviewed.Action != "accept" {
				return CommandProposal{}, fmt.Errorf("command rejected by ACL reviewer")
			}
		case "notify_and_warn":
			proposal.RiskLevel, proposal.RiskReason = raiseRisk(
				proposal.RiskLevel, proposal.RiskReason, 3,
				"command matched a notify-and-warn command ACL",
			)
			forceApproval = true
			r.emitData("data-command-acl", aclDecision, "process")
		case "warning":
			proposal.RiskLevel, proposal.RiskReason = raiseRisk(
				proposal.RiskLevel, proposal.RiskReason, 2,
				"command matched a warning command ACL",
			)
			r.emitData("data-command-acl", aclDecision, "process")
		}
	}
	id := runtimeID("approval")
	digest := proposalDigest(r.terminalID, planID, step.ID, proposal)
	data := map[string]any{
		"id": id, "digest": digest, "planId": planID, "stepId": step.ID,
		"step": index + 1, "total": total, "command": proposal.Command,
		"rationale": proposal.Rationale, "riskLevel": proposal.RiskLevel,
		"riskReason": proposal.RiskReason, "execution": proposal.Execution,
		"executionReason":    proposal.ExecutionCause,
		"backgroundEligible": proposal.BackgroundEligible,
		"approvalThreshold":  threshold, "executionMode": mode,
	}
	if aclDecision.Action != "" && aclDecision.Action != "Unknown" {
		data["commandACL"] = aclDecision
		proposal.CommandACL = &aclDecision
	}
	if proposal.RiskLevel < threshold && !forceApproval {
		data["approvalRequired"] = false
		data["state"] = "auto_approved"
		r.emitData("data-command", data, "process")
		r.writeAudit("command_auto_approved", data)
		return proposal, nil
	}
	data["approvalRequired"] = true
	data["state"] = "awaiting_risk_approval"
	pending := &pendingApproval{
		id: id, digest: digest, proposal: proposal,
		decision: make(chan approvalDecision, 1),
	}
	r.mu.Lock()
	r.pending = pending
	r.mu.Unlock()
	r.emitData("data-approval", data, "process")
	select {
	case <-ctx.Done():
		return CommandProposal{}, ctx.Err()
	case decision := <-pending.decision:
		r.mu.Lock()
		if r.pending == pending {
			r.pending = nil
		}
		r.mu.Unlock()
		if !decision.Approved {
			return CommandProposal{}, context.Canceled
		}
		if mode == ModeAuto && validExecution(decision.Execution) {
			if decision.Execution == ExecutionBackground &&
				!pending.proposal.BackgroundEligible {
				return CommandProposal{}, fmt.Errorf(
					"this command cannot run in the background",
				)
			}
			pending.proposal.Execution = decision.Execution
		}
		r.writeAudit("command_approved", map[string]any{
			"id": id, "digest": digest, "execution": pending.proposal.Execution,
		})
		return pending.proposal, nil
	}
}

func (r *Runtime) resolveApproval(decision approvalDecision) error {
	r.mu.Lock()
	pending := r.pending
	r.mu.Unlock()
	if pending == nil || pending.id != decision.ID || pending.digest != decision.Digest {
		return fmt.Errorf("approval is stale or does not match the pending command")
	}
	select {
	case pending.decision <- decision:
		return nil
	default:
		return fmt.Errorf("approval was already decided")
	}
}

func (r *Runtime) updatePolicy(policy policyUpdate) error {
	r.mu.Lock()
	if policy.ApprovalThreshold != 0 {
		if policy.ApprovalThreshold < 1 || policy.ApprovalThreshold > 4 {
			r.mu.Unlock()
			return fmt.Errorf("approval threshold must be between 1 and 4")
		}
		r.approvalThreshold = policy.ApprovalThreshold
	}
	if policy.ExecutionMode != "" {
		switch policy.ExecutionMode {
		case ModeAuto, ModePTYOnly, ModeBackground:
			r.executionMode = policy.ExecutionMode
		default:
			r.mu.Unlock()
			return fmt.Errorf("invalid execution mode")
		}
	}
	threshold, mode := r.approvalThreshold, r.executionMode
	r.mu.Unlock()
	data := map[string]any{
		"approvalThreshold": threshold, "executionMode": mode,
	}
	r.emitData("data-policy", data, "process")
	r.writeAudit("policy_updated", data)
	return nil
}

func (r *Runtime) execute(
	ctx context.Context, proposal CommandProposal, onOutput func(string),
) (string, *int, error) {
	timeout := defaultExecutionTimeout
	if proposal.Execution == ExecutionBackground {
		if seconds := config.GetConf().TerminalAIBackgroundTimeout; seconds > 0 {
			timeout = time.Duration(seconds) * time.Second
		}
	}
	execCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	if proposal.Execution == ExecutionBackground {
		return r.executeBackground(execCtx, proposal.Command, onOutput)
	}
	r.setInputLocked(true)
	defer r.setInputLocked(false)
	resultCh, err := r.observer.Begin(proposal.Command)
	if err != nil {
		return "", nil, err
	}
	r.mu.Lock()
	authorizePTY := r.authorizePTY
	r.mu.Unlock()
	if authorizePTY != nil {
		authorizePTY(proposal.Command, proposal.CommandACL)
	}
	r.writePTY([]byte(proposal.Command + "\r"))
	select {
	case <-execCtx.Done():
		r.observer.Cancel()
		return r.observer.Snapshot(), nil, execCtx.Err()
	case result := <-resultCh:
		return result.Output, nil, nil
	}
}

func (r *Runtime) executeBackground(
	ctx context.Context, command string, onOutput func(string),
) (string, *int, error) {
	r.mu.Lock()
	backgroundGuard := r.backgroundGuard
	r.mu.Unlock()
	if backgroundGuard != nil {
		if err := backgroundGuard(); err != nil {
			return "", nil, err
		}
	}
	r.executorMu.RLock()
	executor := r.backgroundExecutor
	available := r.backgroundAvailable
	r.executorMu.RUnlock()
	if executor == nil || !available {
		return "", nil, fmt.Errorf("background execution is unavailable")
	}
	executorCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	guardFailure := make(chan error, 1)
	guardDone := make(chan struct{})
	if backgroundGuard != nil {
		go func() {
			ticker := time.NewTicker(15 * time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-executorCtx.Done():
					return
				case <-guardDone:
					return
				case <-ticker.C:
					if err := backgroundGuard(); err != nil {
						select {
						case guardFailure <- err:
						default:
						}
						cancel()
						return
					}
				}
			}
		}()
	}
	output, exitCode, err := executor.Execute(executorCtx, command, func(output string) {
		if backgroundGuard != nil {
			if guardErr := backgroundGuard(); guardErr != nil {
				select {
				case guardFailure <- guardErr:
					cancel()
				default:
				}
				return
			}
		}
		if onOutput != nil {
			onOutput(output)
		}
	})
	close(guardDone)
	select {
	case guardErr := <-guardFailure:
		return output, exitCode, guardErr
	default:
	}
	var unavailable *BackgroundUnavailableError
	if errors.As(err, &unavailable) {
		r.DisableBackground(unavailable.Error())
	}
	return output, exitCode, err
}

func (r *Runtime) applyExecutionMode(proposal *CommandProposal) error {
	r.mu.Lock()
	mode := r.executionMode
	r.mu.Unlock()
	r.executorMu.RLock()
	backgroundAvailable := r.backgroundAvailable
	r.executorMu.RUnlock()
	if !proposal.BackgroundEligible {
		backgroundAvailable = false
	}
	switch mode {
	case ModePTYOnly:
		if proposal.Execution != ExecutionPTY {
			proposal.Execution = ExecutionPTY
			proposal.ExecutionCause = "the terminal is configured for PTY-only execution"
		}
	case ModeBackground:
		if !backgroundAvailable {
			return fmt.Errorf("background-only mode is active but this command cannot run in the background")
		}
		if proposal.Execution != ExecutionBackground {
			proposal.Execution = ExecutionBackground
			proposal.ExecutionCause = "the terminal is configured for background-only execution"
		}
	case ModeAuto:
		if proposal.Execution == ExecutionBackground && !backgroundAvailable {
			proposal.Execution = ExecutionPTY
			proposal.ExecutionCause = "background execution is unavailable; using the active PTY"
		}
	}
	return nil
}

func (r *Runtime) currentProfile(ctx context.Context) AssetProfile {
	select {
	case <-ctx.Done():
		return AssetProfile{DetectionError: ctx.Err().Error()}
	case <-r.profileReady:
		r.mu.Lock()
		defer r.mu.Unlock()
		return r.profile
	case <-time.After(profileTimeout):
		return AssetProfile{DetectionError: "asset profile timed out"}
	}
}

func (r *Runtime) retry(ctx context.Context, call func(context.Context) error) error {
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		callCtx, cancel := context.WithTimeout(ctx, time.Minute)
		lastErr = callWithContext(callCtx, func() error { return call(callCtx) })
		cancel()
		if lastErr == nil || ctx.Err() != nil {
			return lastErr
		}
		if attempt == 2 {
			break
		}
		timer := time.NewTimer(time.Duration(2<<attempt) * time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
	return lastErr
}

func callWithContext(ctx context.Context, call func() error) error {
	done := make(chan error, 1)
	go func() { done <- call() }()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-done:
		return err
	}
}

func (r *Runtime) emitText(text, stage string) {
	message := ChatMessage{
		ID: runtimeID("assistant"), Role: "assistant",
		Metadata: map[string]any{
			"terminalId": r.terminalID, "stage": stage,
		},
		Parts: []ChatPart{{Type: "text", Text: text, State: "done"}},
	}
	r.emit(message)
	r.writeAudit("assistant_text", message)
}

func (r *Runtime) emitData(partType string, data any, stage string) {
	message := ChatMessage{
		ID: runtimeID("assistant"), Role: "assistant",
		Metadata: map[string]any{
			"terminalId": r.terminalID, "stage": stage,
		},
		Parts: []ChatPart{{Type: partType, Data: data}},
	}
	r.emit(message)
	r.writeAudit(partType, data)
}

func (r *Runtime) emitProgress(text, state string, interruptible bool) {
	r.emitData("data-progress", map[string]any{
		"text": text, "state": state, "interruptible": interruptible,
	}, "process")
}

func (r *Runtime) emitError(err error) {
	if err == nil {
		return
	}
	r.emitData("data-error", map[string]any{"message": err.Error()}, "final")
}

func (r *Runtime) emitCapability() {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return
	}
	threshold, mode := r.approvalThreshold, r.executionMode
	r.mu.Unlock()
	r.executorMu.RLock()
	backgroundAvailable := r.backgroundAvailable
	backgroundReason := r.backgroundReason
	r.executorMu.RUnlock()
	r.emitData("data-capability", map[string]any{
		"enabled": true, "ptyExec": true,
		"backgroundExec":    backgroundAvailable,
		"backgroundReason":  backgroundReason,
		"approvalThreshold": threshold, "executionMode": mode,
	}, "process")
}

func (r *Runtime) setInputLocked(locked bool) {
	r.mu.Lock()
	lock := r.inputLock
	r.mu.Unlock()
	if lock != nil {
		lock(locked)
	}
	r.emitData("data-input-lock", map[string]any{"locked": locked}, "process")
}

func (r *Runtime) appendAssistantHistory(value string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.history = append(r.history, "assistant: "+value)
	r.trimHistoryLocked()
}

func (r *Runtime) trimHistoryLocked() {
	total := 0
	index := len(r.history)
	for index > 0 && total < 96*1024 {
		index--
		total += len(r.history[index])
	}
	if index > 0 {
		r.history = append([]string{"system: older conversation compacted"}, r.history[index:]...)
	}
}

func (r *Runtime) Interrupt() {
	r.mu.Lock()
	cancel := r.cancel
	activeExecution := r.activeExecution
	r.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	r.observer.Cancel()
	if activeExecution == ExecutionPTY {
		r.writePTY([]byte{3})
	}
}

func (r *Runtime) Close() {
	r.mu.Lock()
	r.closed = true
	cancel := r.cancel
	audit := r.audit
	lifetimeCancel := r.lifetimeCancel
	inputLock := r.inputLock
	r.mu.Unlock()
	lifetimeCancel()
	if cancel != nil {
		cancel()
	}
	r.wg.Wait()
	r.profileWG.Wait()
	r.executorMu.Lock()
	executor := r.backgroundExecutor
	r.backgroundExecutor = nil
	r.backgroundAvailable = false
	r.executorMu.Unlock()
	if executor != nil {
		_ = executor.Close()
	}
	if inputLock != nil {
		inputLock(false)
	}
	if audit != nil {
		audit.Close()
	}
}

func validateProposal(proposal *CommandProposal) error {
	proposal.Command = strings.TrimSpace(proposal.Command)
	if proposal.Command == "" || len(proposal.Command) > 4096 ||
		strings.ContainsAny(proposal.Command, "\r\n") {
		return fmt.Errorf("model generated an invalid command")
	}
	if proposal.RiskLevel < 1 || proposal.RiskLevel > 4 {
		return fmt.Errorf("model generated an invalid risk level")
	}
	if !validExecution(proposal.Execution) {
		return fmt.Errorf("model generated an invalid execution mode")
	}
	return nil
}

func isInteractiveCommand(command string) bool {
	lower := strings.ToLower(" " + strings.TrimSpace(command) + " ")
	for _, token := range []string{
		" vim ", " vi ", " nano ", " emacs ", " less ", " more ",
		" top ", " htop ", " watch ", " tail -f ", " tail --follow ",
		" journalctl -f ", " journalctl --follow ",
	} {
		if strings.Contains(lower, token) {
			return true
		}
	}
	return false
}

func validExecution(value string) bool {
	return value == ExecutionPTY || value == ExecutionBackground
}

func classifyRisk(command string, modelLevel int, reason string) (int, string) {
	lower := strings.ToLower(command)
	level := modelLevel
	raise := func(minimum int, cause string) {
		if level < minimum {
			level = minimum
			reason = cause
		}
	}
	for _, token := range []string{
		"rm ", "unlink ", "shred ", "mkfs", "fdisk", "parted", "dd if=", "shutdown",
		"reboot", "iptables -f", "nft flush", "userdel", "passwd ",
		"curl | sh", "curl |sh", "wget | sh", "wget |sh",
		"find -delete", " -delete", "git clean", "git reset --hard",
		"truncate ", "wipefs",
	} {
		if strings.Contains(lower, token) {
			raise(4, "backend rule detected a destructive or security-sensitive operation")
		}
	}
	for _, token := range []string{
		"sudo ", " su ", "apt install", "apt-get install", "yum install",
		"dnf install", "apk add", "systemctl ", "service ", "chmod ",
		"chown ", ">/etc/", "tee /etc/",
	} {
		if strings.Contains(" "+lower, token) {
			raise(3, "backend rule detected privilege, installation, or system configuration impact")
		}
	}
	if strings.ContainsAny(command, ">|;") || strings.Contains(lower, " && ") ||
		strings.Contains(lower, " || ") {
		raise(2, "backend rule detected shell composition or output redirection")
	}
	if level == 1 && !isClearlyReadOnly(command) {
		raise(2, "backend could not verify the command as read-only")
	}
	if level < 1 {
		level = 2
	}
	if strings.TrimSpace(reason) == "" {
		reason = "risk classified by the terminal AI backend"
	}
	return level, reason
}

func isClearlyReadOnly(command string) bool {
	fields := strings.Fields(strings.TrimSpace(command))
	if len(fields) == 0 {
		return false
	}
	switch fields[0] {
	case "pwd", "whoami", "id", "uname", "hostname", "date", "uptime",
		"ls", "cat", "head", "tail", "stat", "file",
		"ps", "df", "du", "free", "find", "grep", "awk", "env", "printenv",
		"ip", "ss", "lsof", "journalctl":
		return true
	case "git":
		return len(fields) > 1 && (fields[1] == "status" || fields[1] == "log" ||
			fields[1] == "diff" || fields[1] == "show" || fields[1] == "branch")
	case "systemctl":
		return len(fields) > 1 && (fields[1] == "status" ||
			fields[1] == "is-active" || fields[1] == "is-enabled")
	default:
		return false
	}
}

func raiseRisk(level int, reason string, minimum int, cause string) (int, string) {
	if level < minimum {
		return minimum, cause
	}
	return level, reason
}

func proposalDigest(
	terminalID uint32, planID, stepID string, proposal CommandProposal,
) string {
	value := fmt.Sprintf(
		"%d\x00%s\x00%s\x00%s\x00%d\x00%s",
		terminalID, planID, stepID, proposal.Command,
		proposal.RiskLevel, proposal.Execution,
	)
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func decodePartData(data any, target any) error {
	value, err := json.Marshal(data)
	if err != nil {
		return err
	}
	return json.Unmarshal(value, target)
}

var runtimeSequence atomic.Uint64

func runtimeID(prefix string) string {
	return fmt.Sprintf("%s-%d-%d", prefix, time.Now().UnixMilli(), runtimeSequence.Add(1))
}
