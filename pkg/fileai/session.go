package fileai

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/jumpserver/koko/pkg/common"
	"github.com/jumpserver/koko/pkg/terminalai/provider"
)

const (
	maxFileActions      = 6
	maxDiffDisplayBytes = 32 * 1024
	approvalTimeout     = 5 * time.Minute
)

var errApprovalRejected = errors.New("file action was rejected")

type approvalDecision struct {
	ID       string `json:"id"`
	Digest   string `json:"digest"`
	Decision string `json:"decision"`
}

type pendingApproval struct {
	id       string
	digest   string
	targetID string
	decision chan approvalDecision
}

type builtInSession struct {
	model    *modelClient
	executor Executor
	emit     func(ChatMessage)

	lifetimeCtx context.Context
	cancelLife  context.CancelFunc

	mu        sync.Mutex
	closed    bool
	busy      bool
	cancel    context.CancelFunc
	pending   *pendingApproval
	announced map[string]bool
	wg        sync.WaitGroup
}

func NewSession(options SessionOptions) (Session, error) {
	if options.Executor == nil {
		return nil, fmt.Errorf("file AI executor is required")
	}
	if options.Emit == nil {
		return nil, fmt.Errorf("file AI emitter is required")
	}
	model, err := newModelClient(options.Config, options.Language)
	if err != nil {
		return nil, err
	}
	lifetimeCtx, cancelLife := context.WithCancel(context.Background())
	return &builtInSession{
		model:       model,
		executor:    options.Executor,
		emit:        options.Emit,
		lifetimeCtx: lifetimeCtx,
		cancelLife:  cancelLife,
		announced:   make(map[string]bool),
	}, nil
}

func (s *builtInSession) ProviderInfo() provider.ProviderInfo {
	return s.model.info()
}

func (s *builtInSession) Handle(message ChatMessage) error {
	if message.Role != "user" {
		return fmt.Errorf("only user file AI messages are accepted")
	}
	if domain, _ := message.Metadata["domain"].(string); domain != "file" {
		return fmt.Errorf("file AI message domain must be file")
	}
	targetID, _ := message.Metadata["targetId"].(string)
	targetID = strings.TrimSpace(targetID)
	if targetID == "" {
		return fmt.Errorf("file AI targetId is required")
	}
	for _, part := range message.Parts {
		switch part.Type {
		case "text":
			question := strings.TrimSpace(part.Text)
			if question == "" {
				continue
			}
			s.announceCapability(targetID)
			return s.start(targetID, question, message.Metadata["context"])
		case "data-file-approval":
			var decision approvalDecision
			if err := decodePartData(part.Data, &decision); err != nil {
				return err
			}
			return s.resolveApproval(targetID, decision)
		case "data-interrupt":
			s.interrupt()
			return nil
		}
	}
	return fmt.Errorf("file AI message has no supported part")
}

func (s *builtInSession) start(targetID, question string, fileContext any) error {
	if len(question) > 32*1024 {
		return fmt.Errorf("file AI message is too large")
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return fmt.Errorf("file AI is closed")
	}
	if s.busy {
		s.mu.Unlock()
		return fmt.Errorf("another file AI task is active")
	}
	ctx, cancel := context.WithCancel(s.lifetimeCtx)
	s.busy = true
	s.cancel = cancel
	s.wg.Add(1)
	s.mu.Unlock()
	go func() {
		defer s.wg.Done()
		defer func() {
			s.mu.Lock()
			s.busy = false
			s.cancel = nil
			s.pending = nil
			s.mu.Unlock()
		}()
		if failed := s.run(ctx, targetID, question, fileContext); !failed {
			s.emitProgress(targetID, "", "idle", false)
		}
	}()
	return nil
}

func (s *builtInSession) run(
	ctx context.Context,
	targetID, question string,
	fileContext any,
) bool {
	ctx = provider.WithRequestBudget(ctx, maxFileActions+1)
	s.emitProgress(targetID, "正在分析文件管理请求…", "analyzing", true)
	observations := make([]ActionResult, 0, maxFileActions)
	planID := common.UUID()
	var plan []string
	for round := 1; round <= maxFileActions; round++ {
		decision, err := s.model.decide(ctx, question, fileContext, observations)
		if err != nil {
			if ctx.Err() == nil {
				s.emitError(targetID, err)
				return true
			}
			return false
		}
		if len(plan) == 0 && len(decision.Plan) > 0 {
			plan = append(plan, decision.Plan...)
		}
		if decision.Kind == "answer" {
			if len(plan) > 0 {
				s.emitPlan(targetID, planID, decision.Summary, plan, len(plan)+1, true)
			}
			s.emitText(targetID, decision.Answer)
			return false
		}

		action, err := prepareAction(decision.Action, fileContext)
		if err != nil {
			s.emitError(targetID, err)
			return true
		}
		bindSaveExpectedVersion(&action, observations)
		if len(plan) == 0 {
			step := strings.TrimSpace(decision.Summary)
			if step == "" {
				step = strings.TrimSpace(action.Rationale)
			}
			if step == "" {
				step = action.Tool
			}
			plan = []string{step}
		}
		s.emitPlan(targetID, planID, decision.Summary, plan, round, false)
		s.emitAction(targetID, action, "proposed")
		s.emitProgress(targetID, "正在执行文件工具…", "tool_running", true)
		if action.Tool == ToolSaveText &&
			!hasMatchingSavePrecondition(observations, action.Path, action.ExpectedVersion) {
			s.emitError(targetID, fmt.Errorf(
				"save_text requires a current stat or approved read_text result for the same path",
			))
			return true
		}
		result, runErr := s.executeAction(ctx, targetID, action)
		observations = append(observations, result)
		s.emitResult(targetID, result)
		if errors.Is(runErr, errApprovalRejected) {
			s.emitText(targetID, "文件操作已被拒绝，未对远端文件进行更改。")
			return false
		}
		if runErr != nil && ctx.Err() != nil {
			return false
		}
	}
	s.emitError(targetID, fmt.Errorf("file AI reached the %d action limit", maxFileActions))
	return true
}

func (s *builtInSession) executeAction(
	ctx context.Context,
	targetID string,
	action Action,
) (ActionResult, error) {
	result := ActionResult{
		ID: action.ID, Tool: action.Tool, Path: action.Path,
		Outcome: "success", Summary: action.Tool + " completed",
	}
	var (
		details any
		err     error
	)
	switch action.Tool {
	case ToolListDirectory:
		details, err = s.executor.ListDirectory(
			ctx, action.Path, MaxDirectoryEntries,
		)
	case ToolStat:
		details, err = s.executor.Stat(ctx, action.Path)
	case ToolReadText:
		err = s.approve(
			ctx, targetID, action, 2,
			"读取文件正文并发送给 AI 模型分析",
		)
		if err == nil {
			s.emitAction(targetID, action, "running")
			details, err = s.executor.ReadText(ctx, action.Path, MaxTextBytes)
		}
	case ToolSaveText:
		var before TextResult
		create := action.ExpectedVersion == ExpectedVersionAbsent
		if create {
			_, statErr := s.executor.Stat(ctx, action.Path)
			switch {
			case statErr == nil:
				err = fmt.Errorf("remote file was created before approval")
			case os.IsNotExist(statErr):
				before = TextResult{
					Path: action.Path, Exists: false, Version: ExpectedVersionAbsent,
				}
			default:
				err = statErr
			}
		} else {
			before, err = s.executor.ReadText(ctx, action.Path, MaxTextBytes)
		}
		if err == nil && before.Truncated {
			err = fmt.Errorf("refusing to edit a file whose content was truncated")
		}
		if err == nil && before.Version != action.ExpectedVersion {
			err = fmt.Errorf("remote file changed before approval")
		}
		if err == nil {
			beforeDisplay, beforeTruncated := truncateDisplay(
				before.Content, maxDiffDisplayBytes,
			)
			afterDisplay, afterTruncated := truncateDisplay(
				action.Content, maxDiffDisplayBytes,
			)
			s.emitData(targetID, "data-file-diff", map[string]any{
				"id": action.ID, "path": action.Path,
				"before": beforeDisplay, "after": afterDisplay,
				"beforeVersion":   before.Version,
				"truncated":       beforeTruncated || afterTruncated,
				"beforeTruncated": beforeTruncated,
				"afterTruncated":  afterTruncated,
			}, "process")
			riskReason := "修改远端文件内容"
			if create {
				riskReason = "创建远端文件并写入内容"
			}
			err = s.approve(ctx, targetID, action, 3, riskReason)
		}
		if err == nil {
			s.emitAction(targetID, action, "running")
			details, err = s.executor.SaveText(
				ctx, action.Path, action.Content, action.ExpectedVersion,
			)
		}
	case ToolMkdir:
		err = s.approve(ctx, targetID, action, 3, "在远端创建目录")
		if err == nil {
			s.emitAction(targetID, action, "running")
			err = s.executor.Mkdir(ctx, action.Path)
			details = map[string]any{"path": action.Path}
		}
	case ToolRename:
		var before Entry
		before, err = s.executor.Stat(ctx, action.Path)
		if err == nil {
			err = s.approve(ctx, targetID, action, 3, "重命名远端文件或目录")
		}
		if err == nil {
			var current Entry
			current, err = s.executor.Stat(ctx, action.Path)
			if err == nil && current.Version != before.Version {
				err = fmt.Errorf("remote file changed while awaiting approval")
			}
		}
		if err == nil {
			s.emitAction(targetID, action, "running")
			err = s.executor.Rename(ctx, action.Path, action.DestinationPath)
			details = map[string]any{
				"path": action.Path, "destinationPath": action.DestinationPath,
			}
		}
	case ToolDelete:
		var entry Entry
		entry, err = s.executor.Stat(ctx, action.Path)
		if err == nil && entry.IsDir && !action.Recursive {
			err = fmt.Errorf("recursive=true is required to delete a directory")
		}
		if err == nil {
			riskReason := "删除远端文件"
			if entry.IsDir {
				riskReason = "递归删除远端目录及其内容"
			}
			err = s.approve(ctx, targetID, action, 4, riskReason)
		}
		if err == nil {
			var current Entry
			current, err = s.executor.Stat(ctx, action.Path)
			if err == nil && current.Version != entry.Version {
				err = fmt.Errorf("remote file changed while awaiting approval")
			}
		}
		if err == nil {
			s.emitAction(targetID, action, "running")
			err = s.executor.Delete(ctx, action.Path)
			details = entry
		}
	default:
		err = fmt.Errorf("unsupported file tool %q", action.Tool)
	}
	if err != nil && os.IsNotExist(err) {
		if absentDetails, ok := absentObservationDetails(action); ok {
			details = absentDetails
			result.Summary = action.Tool + " confirmed file does not exist"
			err = nil
		}
	}
	if err != nil {
		result.Outcome = "error"
		result.Summary = action.Tool + " failed"
		result.Error = err.Error()
		if errors.Is(err, errApprovalRejected) {
			result.Outcome = "rejected"
			result.Summary = action.Tool + " rejected"
			s.emitAction(targetID, action, "rejected")
		} else {
			s.emitAction(targetID, action, "failed")
		}
		return result, err
	}
	result.Details = details
	s.emitAction(targetID, action, "completed")
	return result, nil
}

func (s *builtInSession) approve(
	ctx context.Context,
	targetID string,
	action Action,
	riskLevel int,
	riskReason string,
) error {
	id := common.UUID()
	digest := approvalDigest(id, action)
	pending := &pendingApproval{
		id: id, digest: digest, targetID: targetID,
		decision: make(chan approvalDecision, 1),
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return fmt.Errorf("file AI is closed")
	}
	s.pending = pending
	s.mu.Unlock()
	s.emitAction(targetID, action, "awaiting_approval")
	s.emitData(targetID, "data-file-approval", map[string]any{
		"id": id, "digest": digest, "actionId": action.ID,
		"tool": action.Tool, "path": action.Path,
		"destinationPath": action.DestinationPath,
		"summary":         action.Rationale, "riskLevel": riskLevel,
		"riskReason": riskReason, "state": "awaiting_approval",
		"expiresInSeconds": int(approvalTimeout / time.Second),
	}, "process")
	s.emitProgress(targetID, "等待文件操作审批…", "awaiting_approval", true)
	timer := time.NewTimer(approvalTimeout)
	defer timer.Stop()
	defer func() {
		s.mu.Lock()
		if s.pending == pending {
			s.pending = nil
		}
		s.mu.Unlock()
	}()
	select {
	case <-ctx.Done():
		s.emitApprovalResolution(targetID, pending, action, riskLevel, riskReason, "cancelled")
		return ctx.Err()
	case <-timer.C:
		s.emitApprovalResolution(targetID, pending, action, riskLevel, riskReason, "expired")
		return fmt.Errorf("file action approval timed out")
	case decision := <-pending.decision:
		if decision.Decision != "approve" {
			s.emitApprovalResolution(targetID, pending, action, riskLevel, riskReason, "rejected")
			return errApprovalRejected
		}
		s.emitApprovalResolution(targetID, pending, action, riskLevel, riskReason, "approved")
		s.emitProgress(targetID, "文件操作已批准，正在执行…", "executing", true)
		return nil
	}
}

func (s *builtInSession) emitApprovalResolution(
	targetID string,
	pending *pendingApproval,
	action Action,
	riskLevel int,
	riskReason, state string,
) {
	s.emitData(targetID, "data-file-approval", map[string]any{
		"id": pending.id, "digest": pending.digest, "actionId": action.ID,
		"tool": action.Tool, "path": action.Path,
		"destinationPath": action.DestinationPath,
		"summary":         action.Rationale, "riskLevel": riskLevel,
		"riskReason": riskReason, "state": state,
	}, "process")
}

func (s *builtInSession) resolveApproval(
	targetID string,
	decision approvalDecision,
) error {
	s.mu.Lock()
	pending := s.pending
	s.mu.Unlock()
	if pending == nil {
		return fmt.Errorf("no file action is awaiting approval")
	}
	if targetID != pending.targetID || decision.ID != pending.id ||
		decision.Digest == "" || decision.Digest != pending.digest {
		return fmt.Errorf("file action approval does not match the pending action")
	}
	if decision.Decision != "approve" && decision.Decision != "reject" {
		return fmt.Errorf("file action approval decision must be approve or reject")
	}
	select {
	case pending.decision <- decision:
		return nil
	default:
		return fmt.Errorf("file action approval was already resolved")
	}
}

func (s *builtInSession) announceCapability(targetID string) {
	s.mu.Lock()
	if s.announced[targetID] {
		s.mu.Unlock()
		return
	}
	s.announced[targetID] = true
	s.mu.Unlock()
	info := s.model.info()
	s.emitData(targetID, "data-capability", map[string]any{
		"enabled": true,
		"tools": []string{
			ToolListDirectory, ToolStat, ToolReadText, ToolSaveText,
			ToolMkdir, ToolRename, ToolDelete,
		},
		"maxDirectoryEntries": MaxDirectoryEntries,
		"maxTextBytes":        MaxTextBytes,
		"provider":            info.Name,
		"model":               info.Model,
		"modelCapabilities":   info.Capabilities,
	}, "process")
}

func (s *builtInSession) emitPlan(
	targetID, id, summary string,
	plan []string,
	round int,
	completed bool,
) {
	steps := make([]map[string]any, 0, len(plan))
	for index, value := range plan {
		status := "pending"
		switch {
		case completed || index < round-1:
			status = "completed"
		case index == min(round-1, len(plan)-1):
			status = "in_progress"
		}
		steps = append(steps, map[string]any{
			"id":    fmt.Sprintf("%s-step-%d", id, index+1),
			"title": value, "objective": value, "status": status,
		})
	}
	s.emitData(targetID, "data-plan", map[string]any{
		"id": id, "summary": summary, "steps": steps,
	}, "process")
}

func (s *builtInSession) emitAction(targetID string, action Action, state string) {
	riskLevel, riskReason := actionRisk(action)
	s.emitData(targetID, "data-file-action", map[string]any{
		"id": action.ID, "tool": action.Tool, "path": action.Path,
		"destinationPath": action.DestinationPath,
		"rationale":       action.Rationale, "riskLevel": riskLevel,
		"riskReason": riskReason, "state": state,
	}, "process")
}

func (s *builtInSession) emitResult(targetID string, result ActionResult) {
	s.emitData(targetID, "data-file-result", result, "process")
}

func (s *builtInSession) emitProgress(
	targetID, text, state string,
	interruptible bool,
) {
	s.emitData(targetID, "data-progress", map[string]any{
		"text": text, "state": state, "interruptible": interruptible,
	}, "process")
}

func (s *builtInSession) emitText(targetID, value string) {
	s.emit(ChatMessage{
		ID: common.UUID(), Role: "assistant",
		Metadata: map[string]any{
			"domain": "file", "targetId": targetID, "stage": "final",
		},
		Parts: []ChatPart{{Type: "text", Text: value}},
	})
}

func (s *builtInSession) emitError(targetID string, err error) {
	if err == nil {
		return
	}
	s.emitData(targetID, "data-error", map[string]any{
		"message": err.Error(),
	}, "final")
}

func (s *builtInSession) emitData(
	targetID, partType string,
	data any,
	stage string,
) {
	s.emit(ChatMessage{
		ID: common.UUID(), Role: "assistant",
		Metadata: map[string]any{
			"domain": "file", "targetId": targetID, "stage": stage,
		},
		Parts: []ChatPart{{Type: partType, Data: data}},
	})
}

func (s *builtInSession) interrupt() {
	s.mu.Lock()
	cancel := s.cancel
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (s *builtInSession) Cancel() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	cancel := s.cancel
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	s.cancelLife()
}

func (s *builtInSession) Close() {
	s.Cancel()
	s.wg.Wait()
}

func prepareAction(action Action, fileContext any) (Action, error) {
	action.ID = common.UUID()
	if strings.TrimSpace(action.Path) == "" && action.Tool == ToolListDirectory {
		action.Path = contextCurrentPath(fileContext)
	}
	path, err := cleanPath(action.Path)
	if err != nil {
		return action, err
	}
	if isWriteTool(action.Tool) && path == "." {
		return action, fmt.Errorf("refusing to modify the current directory")
	}
	if !filepath.IsAbs(path) {
		currentPath := contextCurrentPath(fileContext)
		if currentPath != "" && currentPath != "/" {
			currentPath, err = cleanPath(currentPath)
			if err != nil || !filepath.IsAbs(currentPath) {
				return action, fmt.Errorf("invalid file UI current path")
			}
			path = filepath.Join(currentPath, path)
		}
	}
	action.Path = path
	if isWriteTool(action.Tool) && (path == "/" || path == ".") {
		return action, fmt.Errorf("refusing to modify the file root")
	}
	if action.Tool == ToolRename {
		destination := strings.TrimSpace(action.DestinationPath)
		if filepath.Base(destination) == destination {
			destination = filepath.Join(filepath.Dir(path), destination)
		}
		destination, err = cleanPath(destination)
		if err != nil {
			return action, fmt.Errorf("invalid rename destination: %w", err)
		}
		if filepath.Clean(filepath.Dir(destination)) != filepath.Clean(filepath.Dir(path)) ||
			destination == path {
			return action, fmt.Errorf("rename destination must be a different name in the same directory")
		}
		action.DestinationPath = destination
	}
	if action.Tool == ToolSaveText {
		if len(action.Content) > MaxTextBytes {
			return action, fmt.Errorf("file content exceeds the file AI text limit")
		}
		if !utf8.ValidString(action.Content) || strings.IndexByte(action.Content, 0) >= 0 {
			return action, fmt.Errorf("save_text accepts UTF-8 text only")
		}
	}
	return action, nil
}

func cleanPath(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" || strings.IndexByte(value, 0) >= 0 {
		return "", fmt.Errorf("file path is required")
	}
	for _, part := range strings.Split(filepath.ToSlash(value), "/") {
		if part == ".." {
			return "", fmt.Errorf("parent path segments are not allowed")
		}
	}
	return filepath.Clean(value), nil
}

func contextCurrentPath(value any) string {
	contextMap, ok := value.(map[string]any)
	if !ok {
		return ""
	}
	path, _ := contextMap["currentPath"].(string)
	return strings.TrimSpace(path)
}

func isWriteTool(tool string) bool {
	switch tool {
	case ToolSaveText, ToolMkdir, ToolRename, ToolDelete:
		return true
	default:
		return false
	}
}

func actionRisk(action Action) (int, string) {
	switch action.Tool {
	case ToolDelete:
		return 4, "删除远端文件或目录"
	case ToolSaveText:
		if action.ExpectedVersion == ExpectedVersionAbsent {
			return 3, "创建远端文件并写入内容"
		}
		return 3, "修改远端文件内容"
	case ToolMkdir:
		return 3, "创建远端目录"
	case ToolRename:
		return 3, "重命名远端文件或目录"
	case ToolReadText:
		return 2, "读取文件正文并发送给 AI 模型分析"
	default:
		return 1, "只读文件检查"
	}
}

func bindSaveExpectedVersion(action *Action, results []ActionResult) {
	if action.Tool != ToolSaveText || strings.TrimSpace(action.ExpectedVersion) != "" {
		return
	}
	if version, ok := observedSaveVersion(results, action.Path); ok {
		action.ExpectedVersion = version
	}
}

func hasMatchingSavePrecondition(results []ActionResult, path, version string) bool {
	observedVersion, ok := observedSaveVersion(results, path)
	return ok && strings.TrimSpace(version) != "" && observedVersion == version
}

func observedSaveVersion(results []ActionResult, path string) (string, bool) {
	for index := len(results) - 1; index >= 0; index-- {
		result := results[index]
		if result.Path != path || (result.Tool != ToolReadText && result.Tool != ToolStat) {
			continue
		}
		if result.Outcome != "success" {
			return "", false
		}
		switch result.Tool {
		case ToolReadText:
			text, ok := result.Details.(TextResult)
			if !ok || text.Truncated || strings.TrimSpace(text.Version) == "" {
				return "", false
			}
			if (text.Exists && text.Version == ExpectedVersionAbsent) ||
				(!text.Exists && text.Version != ExpectedVersionAbsent) {
				return "", false
			}
			return text.Version, true
		case ToolStat:
			entry, ok := result.Details.(Entry)
			if ok && !entry.Exists && entry.Version == ExpectedVersionAbsent {
				return ExpectedVersionAbsent, true
			}
			return "", false
		}
	}
	return "", false
}

func absentObservationDetails(action Action) (any, bool) {
	switch action.Tool {
	case ToolStat:
		return Entry{
			Name: filepath.Base(action.Path), Path: action.Path,
			Exists: false, Version: ExpectedVersionAbsent,
		}, true
	case ToolReadText:
		return TextResult{
			Path: action.Path, Exists: false, Version: ExpectedVersionAbsent,
		}, true
	default:
		return nil, false
	}
}

func truncateDisplay(value string, limit int) (string, bool) {
	if len(value) <= limit {
		return value, false
	}
	value = value[:limit]
	for len(value) > 0 && !utf8.ValidString(value) {
		value = value[:len(value)-1]
	}
	return value, true
}

func approvalDigest(id string, action Action) string {
	payload, _ := json.Marshal(action)
	hash := sha256.Sum256(append(append([]byte(id), 0), payload...))
	return hex.EncodeToString(hash[:])
}

func decodePartData(value any, output any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, output)
}
