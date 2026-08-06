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
	maxDecisionText          = 64 * 1024
	maxPlanSummary           = 16 * 1024
	maxStepTitle             = 512
	maxStepObjective         = 4 * 1024
	maxProposalExplanation   = 8 * 1024
	maxReviewSummary         = 16 * 1024
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
	ruleResolution    RuleResolution

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
	if aware, ok := adapter.(interface {
		RuleResolution() RuleResolution
	}); ok {
		r.applyRuleResolution(aware.RuleResolution())
	}
	if !needsProfileDetection(adapter) || !adapter.SupportsBackground() {
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
	if profile.PlatformFamily == "" {
		profile.PlatformFamily = r.profile.PlatformFamily
	}
	if profile.SessionContext.Protocol == "" {
		profile.SessionContext = r.profile.SessionContext
	}
	r.profile = profile
	adapter := r.adapter
	r.mu.Unlock()
	if aware, ok := adapter.(interface {
		UpdateProfile(AssetProfile) RuleResolution
	}); ok {
		r.applyRuleResolution(aware.UpdateProfile(profile))
	}
	r.profileReadyOnce.Do(func() { close(r.profileReady) })
}

func (r *Runtime) applyRuleResolution(resolution RuleResolution) {
	r.mu.Lock()
	r.ruleResolution = resolution
	model := r.model
	if resolution.disablesBackground() {
		r.executionMode = ModePTYOnly
	}
	r.mu.Unlock()
	var executor BackgroundExecutor
	if resolution.disablesBackground() {
		r.executorMu.Lock()
		executor = r.backgroundExecutor
		r.backgroundExecutor = nil
		r.backgroundAvailable = false
		r.backgroundReason = "background execution is disabled by Terminal AI rules"
		r.executorMu.Unlock()
	}
	if executor != nil {
		_ = executor.Close()
	}
	if aware, ok := model.(RulePolicyModel); ok {
		aware.SetPolicyInstructions(resolution.PromptInstructions)
	}
	r.writeAudit("rules_resolved", resolution)
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
	ctx = withModelRequestBudget(ctx, maxModelRequests)
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
	historyEntries := r.history
	if len(historyEntries) > 0 {
		historyEntries = historyEntries[:len(historyEntries)-1]
	}
	history := strings.Join(historyEntries, "\n")
	mode := r.executionMode
	r.mu.Unlock()
	r.emitProgress("正在分析请求…", "analyzing", true)
	decision, err := r.initialDecision(
		ctx, question, history, profile.String(), snapshot,
	)
	if err != nil {
		r.emitError(err)
		return
	}
	if decision.Kind == "answer" {
		r.emitText(decision.Answer, "final")
		r.appendAssistantHistory(decision.Answer)
		return
	}
	planID := runtimeID("plan")
	for index := range decision.Steps {
		decision.Steps[index].ID = fmt.Sprintf("%s-step-%d", planID, index+1)
		decision.Steps[index].ParentStepID = ""
		decision.Steps[index].Status = StepPending
		decision.Steps[index].rootStepID = decision.Steps[index].ID
	}
	plan := newReActPlan(planID, decision.Summary, decision.Steps)
	r.emitPlan(plan, 0, "")
	r.mu.Lock()
	adapter := r.adapter
	r.mu.Unlock()
	if adapter == nil {
		r.finishReAct(ctx, question, plan, 0, "terminal AI adapter is unavailable")
		return
	}
	for round := 1; round <= maxReActRounds; round++ {
		if ctx.Err() != nil {
			return
		}
		r.emitProgress(
			"正在更新执行计划…",
			"planning", true,
		)
		r.executorMu.RLock()
		backgroundAvailable := r.backgroundAvailable
		r.executorMu.RUnlock()
		snapshot = r.observer.Snapshot()
		next, transition, err := r.nextReActDecision(
			ctx, ReActRequest{
				Question: question, PlanSummary: plan.summary,
				Steps: plan.steps, Results: plan.results,
				Profile: profile.String(), Snapshot: snapshot,
				Mode: mode, BackgroundAvailable: backgroundAvailable,
				Round: round, MaxRounds: maxReActRounds,
			},
			plan, adapter,
		)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			r.emitError(err)
			r.finishReAct(ctx, question, plan, round, err.Error())
			return
		}
		if next.Kind == ReActFinish {
			plan.commit(transition)
			r.emitObservation(plan, next.Observation)
			r.emitPlan(plan, round, next.ThoughtSummary)
			r.emitText(next.Summary, "final")
			r.appendAssistantHistory(next.Summary)
			return
		}
		if err = plan.beginExecution(transition); err != nil {
			r.emitError(err)
			r.finishReAct(ctx, question, plan, round, err.Error())
			return
		}
		proposal := *next.Proposal
		step, index := findStep(plan.steps, transition.nextStepID)
		r.emitObservation(plan, next.Observation)
		r.emitPlan(plan, round, next.ThoughtSummary)
		approvedProposal, err := r.authorize(
			ctx, planID, step, index, len(plan.steps), proposal,
		)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			plan.rejectExecution(step.ID, proposal, err.Error())
			r.emitPlan(plan, round, next.ThoughtSummary)
			r.finishReAct(ctx, question, plan, round, err.Error())
			return
		}
		proposal = approvedProposal
		progress := "正在执行命令…"
		if proposal.Execution == ExecutionPTY {
			progress = "命令正在当前 PTY 中执行，可直接在终端交互…"
		}
		r.emitProgress(progress, "executing", true)
		r.mu.Lock()
		r.activeExecution = proposal.Execution
		r.mu.Unlock()
		output, exitCode, err := r.execute(ctx, proposal, func(output string) {
			r.emitData("data-execution", map[string]any{
				"planId": planID, "stepId": step.ID,
				"step": index + 1, "total": len(plan.steps),
				"command": proposal.Command, "execution": proposal.Execution,
				"output": output, "outcome": "running",
			}, "process")
		})
		r.mu.Lock()
		r.activeExecution = ""
		backgroundRecord := r.backgroundRecord
		r.mu.Unlock()
		executionErr := err
		if proposal.Execution == ExecutionBackground && backgroundRecord != nil {
			if err != nil {
				output = strings.TrimSpace(output + "\n" + err.Error())
			}
			backgroundRecord(proposal.Command, output, exitCode, proposal.CommandACL)
		} else if err != nil {
			output = strings.TrimSpace(output + "\n" + err.Error())
		}
		if recordErr := plan.recordExecution(
			step.ID, proposal, output, exitCode, executionErr,
		); recordErr != nil {
			r.emitError(recordErr)
			r.finishReAct(ctx, question, plan, round, recordErr.Error())
			return
		}
		r.emitData("data-execution", map[string]any{
			"planId": planID, "stepId": step.ID,
			"step": index + 1, "total": len(plan.steps),
			"command": proposal.Command, "execution": proposal.Execution,
			"output": output, "outputTruncated": outputIsTruncated(output),
			"exitCode": exitCode, "outcome": "reviewing",
		}, "process")
		r.emitPlan(plan, round, next.ThoughtSummary)
	}
	r.finishReAct(ctx, question, plan, maxReActRounds, "达到 20 轮 ReAct 上限")
}

func (r *Runtime) initialDecision(
	ctx context.Context,
	question, history, profile, snapshot string,
) (Decision, error) {
	var decision Decision
	correction := ""
	for repair := 0; repair < 2; repair++ {
		err := r.retry(ctx, func(callCtx context.Context) error {
			var callErr error
			decision, callErr = r.model.Decide(
				callCtx, question, history, profile, snapshot, correction,
			)
			return callErr
		})
		if err != nil {
			var outputErr *ModelOutputError
			if repair == 0 && errors.As(err, &outputErr) {
				correction = err.Error()
				continue
			}
			return Decision{}, err
		}
		if err = validateDecision(decision); err == nil {
			return decision, nil
		} else if repair == 0 {
			correction = err.Error()
			continue
		}
		return Decision{}, err
	}
	return Decision{}, fmt.Errorf("model failed to produce an initial decision")
}

func (r *Runtime) nextReActDecision(
	ctx context.Context,
	request ReActRequest,
	plan *reactPlan,
	adapter Adapter,
) (ReActDecision, reactTransition, error) {
	var decision ReActDecision
	var transition reactTransition
	correction := ""
	for repair := 0; repair < 2; repair++ {
		request.Correction = correction
		err := r.retry(ctx, func(callCtx context.Context) error {
			var callErr error
			decision, callErr = r.model.Next(callCtx, request)
			return callErr
		})
		if err != nil {
			var outputErr *ModelOutputError
			if repair == 0 && errors.As(err, &outputErr) {
				correction = err.Error()
				continue
			}
			return decision, transition, err
		}
		if decision.Kind == ReActExecute {
			if decision.Proposal == nil {
				err = fmt.Errorf("model execute action has no proposal")
			} else {
				proposal := *decision.Proposal
				if err = validateProposal(&proposal); err == nil {
					err = adapter.PrepareProposal(&proposal)
					r.writeCommandRuleAudit(proposal, err)
				}
				if err == nil {
					err = r.applyExecutionMode(&proposal)
				}
				if err == nil {
					err = validateProposal(&proposal)
				}
				decision.Proposal = &proposal
			}
		}
		if err == nil && decision.Kind == ReActFinish &&
			reviewingOutputIsIncomplete(request.Results) {
			err = fmt.Errorf(
				"cannot finish from a truncated command result; execute a " +
					"bounded follow-up command and verify completeness",
			)
		}
		if err == nil {
			transition, err = plan.preview(decision)
		}
		if err == nil {
			return decision, transition, nil
		}
		if repair == 0 {
			correction = err.Error()
			continue
		}
		return decision, transition, err
	}
	return decision, transition, fmt.Errorf("model failed to produce a valid ReAct action")
}

func (r *Runtime) finishReAct(
	ctx context.Context,
	question string,
	plan *reactPlan,
	round int,
	reason string,
) {
	if ctx.Err() != nil {
		return
	}
	plan.forceStop(reason)
	r.emitLatestResult(plan)
	r.emitPlan(plan, round, "执行已停止，正在整理结果")
	r.emitProgress("正在生成执行总结…", "summarizing", true)
	summary := ""
	if modelRequestUsage(ctx) < maxModelRequests {
		err := r.retry(ctx, func(callCtx context.Context) error {
			var callErr error
			summary, callErr = r.model.Summarize(
				callCtx, question, plan.summary, plan.steps, plan.results, reason,
			)
			return callErr
		})
		if err != nil || len(summary) == 0 || len(summary) > maxDecisionText {
			summary = localReActSummary(plan, reason)
		}
	} else {
		summary = localReActSummary(plan, reason)
	}
	r.emitText(summary, "final")
	r.appendAssistantHistory(summary)
}

func (r *Runtime) emitPlan(plan *reactPlan, round int, thinking string) {
	r.emitData("data-plan", map[string]any{
		"id": plan.id, "summary": plan.summary,
		"steps": plan.steps, "round": round,
		"maxRounds": maxReActRounds, "thinking": thinking,
	}, "process")
}

func (r *Runtime) emitObservation(
	plan *reactPlan, review ObservationReview,
) {
	if review.Outcome == "none" {
		return
	}
	for _, result := range plan.results {
		if result.StepID == review.StepID {
			r.emitResult(plan, result)
			return
		}
	}
}

func (r *Runtime) emitLatestResult(plan *reactPlan) {
	if len(plan.results) == 0 {
		return
	}
	r.emitResult(plan, plan.results[len(plan.results)-1])
}

func (r *Runtime) emitResult(plan *reactPlan, result StepResult) {
	_, index := findStep(plan.steps, result.StepID)
	r.emitData("data-execution", map[string]any{
		"planId": plan.id, "stepId": result.StepID,
		"step": index + 1, "total": len(plan.steps),
		"command": result.Command, "execution": result.Execution,
		"output": result.Output, "exitCode": result.ExitCode,
		"outputTruncated": result.OutputTruncated,
		"outcome":         result.Status, "summary": result.Summary,
		"errorReason": result.ErrorReason,
	}, "process")
}

func findStep(steps []Step, stepID string) (Step, int) {
	for index, step := range steps {
		if step.ID == stepID {
			return step, index
		}
	}
	return Step{}, -1
}

func localReActSummary(plan *reactPlan, reason string) string {
	counts := make(map[string]int)
	for _, step := range plan.steps {
		counts[step.Status]++
	}
	return fmt.Sprintf(
		"任务已停止：%s。\n\n执行结果：完成 %d 步，失败 %d 步，拒绝 %d 步，未执行 %d 步。",
		reason, counts[StepCompleted], counts[StepFailed],
		counts[StepRejected], counts[StepSkipped],
	)
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
	requiresACLReview := false
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
			if aclReview == nil {
				return CommandProposal{}, fmt.Errorf("command ACL review is unavailable")
			}
			forceApproval = true
			requiresACLReview = true
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
		"executionReason":        proposal.ExecutionCause,
		"backgroundEligible":     proposal.BackgroundEligible,
		"policyApprovalRequired": proposal.ApprovalRequired,
		"approvalThreshold":      threshold, "executionMode": mode,
	}
	if aclDecision.Action != "" && aclDecision.Action != "Unknown" {
		data["commandACL"] = aclDecision
		proposal.CommandACL = &aclDecision
	}
	if !requiresRiskApproval(proposal, threshold, forceApproval) {
		data["approvalRequired"] = false
		data["state"] = "auto_approved"
		r.emitData("data-command", data, "process")
		r.writeAudit("command_auto_approved", data)
		return r.completeCommandACLReview(
			ctx, proposal, aclDecision, aclReview, requiresACLReview,
		)
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
			return CommandProposal{}, errors.New("command approval was rejected")
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
		return r.completeCommandACLReview(
			ctx, pending.proposal, aclDecision, aclReview, requiresACLReview,
		)
	}
}

func (r *Runtime) completeCommandACLReview(
	ctx context.Context,
	proposal CommandProposal,
	decision CommandACLDecision,
	review func(context.Context, CommandACLDecision, string) (CommandACLDecision, error),
	required bool,
) (CommandProposal, error) {
	if !required {
		return proposal, nil
	}
	if err := ctx.Err(); err != nil {
		return CommandProposal{}, err
	}
	r.emitData("data-command-acl", map[string]any{
		"state": "waiting_for_review", "command": proposal.Command,
		"decision": decision,
	}, "process")
	reviewed, err := review(ctx, decision, proposal.Command)
	if err != nil {
		return CommandProposal{}, err
	}
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
	proposal.CommandACL = &reviewed
	return proposal, nil
}

func requiresRiskApproval(
	proposal CommandProposal,
	threshold int,
	forceApproval bool,
) bool {
	return proposal.ApprovalRequired || forceApproval ||
		proposal.RiskLevel >= threshold
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
	if proposal.MaxExecutionSeconds > 0 {
		ruleTimeout := time.Duration(proposal.MaxExecutionSeconds) * time.Second
		if ruleTimeout < timeout {
			timeout = ruleTimeout
		}
	}
	execCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	if proposal.Execution == ExecutionBackground {
		return r.executeBackground(execCtx, proposal.Command, onOutput)
	}
	r.setInputLocked(true)
	inputLocked := true
	defer func() {
		if inputLocked {
			r.setInputLocked(false)
		}
	}()
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
	r.setInputLocked(false)
	inputLocked = false
	select {
	case <-execCtx.Done():
		r.observer.Cancel()
		return r.observer.Snapshot(), nil, execCtx.Err()
	case result := <-resultCh:
		return result.Output, nil, nil
	}
}

func (r *Runtime) writeCommandRuleAudit(
	proposal CommandProposal,
	decisionErr error,
) {
	if len(proposal.RuleMatches) == 0 &&
		len(proposal.DeniedByRules) == 0 {
		return
	}
	payload := map[string]any{
		"command":             proposal.Command,
		"matches":             proposal.RuleMatches,
		"minimumRiskResult":   proposal.RiskLevel,
		"approvalRequired":    proposal.ApprovalRequired,
		"execution":           proposal.Execution,
		"maxExecutionSeconds": proposal.MaxExecutionSeconds,
		"policy":              proposal.RulePolicy,
	}
	if len(proposal.DeniedByRules) > 0 {
		payload["deniedBy"] = proposal.DeniedByRules
	}
	if decisionErr != nil {
		payload["error"] = decisionErr.Error()
	}
	r.writeAudit("command_rules_applied", payload)
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
		callCtx, cancel := context.WithTimeout(ctx, modelRequestTimeout)
		lastErr = callWithContext(callCtx, func() error { return call(callCtx) })
		cancel()
		if lastErr == nil || ctx.Err() != nil {
			return lastErr
		}
		if attempt == 2 || !retryableModelError(lastErr) {
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
	if errors.Is(lastErr, context.DeadlineExceeded) {
		return fmt.Errorf(
			"terminal AI model request timed out after %s",
			modelRequestTimeout,
		)
	}
	return lastErr
}

func retryableModelError(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	var requestErr *ProviderRequestError
	return errors.As(err, &requestErr) && requestErr.Retryable
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
	capability := map[string]any{
		"enabled": true, "ptyExec": true,
		"backgroundExec":    backgroundAvailable,
		"backgroundReason":  backgroundReason,
		"approvalThreshold": threshold, "executionMode": mode,
	}
	if provider, ok := r.model.(ModelProviderInfo); ok {
		info := provider.ProviderInfo()
		capability["provider"] = info.Name
		capability["model"] = info.Model
		capability["modelCapabilities"] = info.Capabilities
	}
	r.emitData("data-capability", capability, "process")
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
	for index > 0 && total < maxModelHistory {
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
	proposal.RiskLevel, proposal.RiskReason = normalizeRisk(
		proposal.RiskLevel, proposal.RiskReason,
	)
	if proposal.Command == "" || len(proposal.Command) > 4096 ||
		strings.ContainsAny(proposal.Command, "\r\n") {
		return fmt.Errorf("model generated an invalid command")
	}
	if !validExecution(proposal.Execution) {
		return fmt.Errorf("model generated an invalid execution mode")
	}
	if len(proposal.Rationale) > maxProposalExplanation ||
		len(proposal.RiskReason) > maxProposalExplanation ||
		len(proposal.ExecutionCause) > maxProposalExplanation {
		return fmt.Errorf("model generated oversized command metadata")
	}
	return nil
}

func validateDecision(decision Decision) error {
	switch decision.Kind {
	case "answer":
		if len(decision.Answer) == 0 || len(decision.Answer) > maxDecisionText {
			return fmt.Errorf("model returned an invalid answer")
		}
	case "plan":
		if len(decision.Summary) > maxPlanSummary ||
			len(decision.Steps) == 0 || len(decision.Steps) > 20 {
			return fmt.Errorf("model returned an invalid plan")
		}
		for _, step := range decision.Steps {
			if len(step.Title) == 0 || len(step.Title) > maxStepTitle ||
				len(step.Objective) == 0 || len(step.Objective) > maxStepObjective {
				return fmt.Errorf("model returned an invalid plan step")
			}
		}
	default:
		return fmt.Errorf("model returned an invalid decision")
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
