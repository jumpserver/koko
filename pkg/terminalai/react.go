package terminalai

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
)

const (
	maxReActRounds        = 20
	maxPendingSteps       = 20
	maxStepAttempts       = 3
	maxSameCommandContext = 2
	maxStepReference      = 256
	maxThoughtSummary     = 2 * 1024
)

type reactPlan struct {
	id             string
	summary        string
	steps          []Step
	results        []StepResult
	commandRepeats map[string]int
}

type reactTransition struct {
	steps             []Step
	results           []StepResult
	nextStepID        string
	commandContextKey string
}

func newReActPlan(id, summary string, steps []Step) *reactPlan {
	plan := &reactPlan{
		id:             id,
		summary:        summary,
		steps:          cloneSteps(steps),
		commandRepeats: make(map[string]int),
	}
	for index := range plan.steps {
		if plan.steps[index].rootStepID == "" {
			plan.steps[index].rootStepID = plan.steps[index].ID
		}
	}
	return plan
}

func (p *reactPlan) preview(decision ReActDecision) (reactTransition, error) {
	transition := reactTransition{
		steps:   cloneSteps(p.steps),
		results: cloneResults(p.results),
	}
	if len(decision.ThoughtSummary) == 0 ||
		len(decision.ThoughtSummary) > maxThoughtSummary {
		return transition, fmt.Errorf("model returned an invalid decision summary")
	}
	if err := applyObservation(
		transition.steps, transition.results, decision.Observation,
	); err != nil {
		return transition, err
	}
	var err error
	transition.steps, transition.nextStepID, err = revisePendingSteps(
		transition.steps, decision.Steps, decision.NextStepID,
	)
	if err != nil {
		return transition, err
	}
	switch decision.Kind {
	case ReActExecute:
		if decision.Proposal == nil || decision.Summary != "" {
			return transition, fmt.Errorf("model returned an invalid execute action")
		}
		if transition.nextStepID == "" {
			return transition, fmt.Errorf("model execute action has no next step")
		}
		transition.commandContextKey, err = p.validateExecution(
			transition.steps, transition.results,
			transition.nextStepID, *decision.Proposal,
		)
		if err != nil {
			return transition, err
		}
	case ReActFinish:
		if decision.Proposal != nil {
			return transition, fmt.Errorf("finish action proposal must be null")
		}
		if decision.NextStepID != "" {
			return transition, fmt.Errorf("finish action nextStepId must be empty")
		}
		if len(decision.Summary) == 0 || len(decision.Summary) > maxDecisionText {
			return transition, fmt.Errorf("finish action summary is invalid")
		}
		for index := range transition.steps {
			if transition.steps[index].Status == StepPending {
				transition.steps[index].Status = StepSkipped
			}
		}
	default:
		return transition, fmt.Errorf("model returned an invalid ReAct action")
	}
	return transition, nil
}

func (p *reactPlan) validateExecution(
	steps []Step,
	results []StepResult,
	stepID string,
	proposal CommandProposal,
) (string, error) {
	if len(results) >= maxReActRounds {
		return "", fmt.Errorf("maximum execute step count reached")
	}
	var step Step
	found := false
	for _, candidate := range steps {
		if candidate.ID == stepID && candidate.Status == StepPending {
			step = candidate
			found = true
			break
		}
	}
	if !found {
		return "", fmt.Errorf("model selected a step that is not pending")
	}
	rootID := step.rootStepID
	if rootID == "" {
		rootID = step.ID
	}
	attempts := 0
	for _, result := range results {
		for _, priorStep := range steps {
			if priorStep.ID == result.StepID && priorStep.rootStepID == rootID {
				attempts++
				break
			}
		}
	}
	if attempts >= maxStepAttempts {
		return "", fmt.Errorf(
			"logical step %q reached the %d-attempt limit",
			step.Title, maxStepAttempts,
		)
	}
	contextKey := commandContextKey(
		rootID, proposal.Command, latestObservationFingerprint(results),
	)
	if p.commandRepeats[contextKey] >= maxSameCommandContext {
		return "", fmt.Errorf(
			"the same command was already attempted %d times without material context change",
			maxSameCommandContext,
		)
	}
	return contextKey, nil
}

func (p *reactPlan) commit(transition reactTransition) {
	p.steps = transition.steps
	p.results = transition.results
}

func (p *reactPlan) beginExecution(
	transition reactTransition,
) error {
	p.commit(transition)
	for index := range p.steps {
		if p.steps[index].ID == transition.nextStepID &&
			p.steps[index].Status == StepPending {
			p.steps[index].Status = StepInProgress
			p.commandRepeats[transition.commandContextKey]++
			return nil
		}
	}
	return fmt.Errorf("next execute step is unavailable")
}

func (p *reactPlan) recordExecution(
	stepID string,
	proposal CommandProposal,
	output string,
	exitCode *int,
	executionErr error,
) error {
	for index := range p.steps {
		if p.steps[index].ID != stepID ||
			p.steps[index].Status != StepInProgress {
			continue
		}
		p.steps[index].Status = StepReviewing
		result := StepResult{
			StepID: stepID, Command: proposal.Command,
			Output: output, Status: StepReviewing,
			OutputTruncated: outputIsTruncated(output),
			Execution:       proposal.Execution, ExitCode: exitCode,
		}
		if executionErr != nil {
			result.ErrorReason = executionErr.Error()
		}
		p.results = append(p.results, result)
		return nil
	}
	return fmt.Errorf("executed step is unavailable")
}

func (p *reactPlan) rejectExecution(
	stepID string,
	proposal CommandProposal,
	reason string,
) {
	for index := range p.steps {
		if p.steps[index].ID == stepID {
			p.steps[index].Status = StepRejected
			break
		}
	}
	p.results = append(p.results, StepResult{
		StepID: stepID, Command: proposal.Command,
		Status: StepRejected, Summary: reason,
		ErrorReason: reason, Execution: proposal.Execution,
	})
}

func (p *reactPlan) forceStop(reason string) {
	for index := range p.steps {
		switch p.steps[index].Status {
		case StepPending:
			p.steps[index].Status = StepSkipped
		case StepInProgress:
			p.steps[index].Status = StepFailed
		case StepReviewing:
			status := StepCompleted
			for resultIndex := range p.results {
				result := &p.results[resultIndex]
				if result.StepID != p.steps[index].ID ||
					result.Status != StepReviewing {
					continue
				}
				if result.ErrorReason != "" ||
					(result.ExitCode != nil && *result.ExitCode != 0) {
					status = StepFailed
				}
				result.Status = status
				if result.Summary == "" {
					result.Summary = reason
				}
				break
			}
			p.steps[index].Status = status
		}
	}
}

func applyObservation(
	steps []Step,
	results []StepResult,
	review ObservationReview,
) error {
	reviewingResult := -1
	for index := range results {
		if results[index].Status == StepReviewing {
			if reviewingResult >= 0 {
				return fmt.Errorf("multiple observations are awaiting review")
			}
			reviewingResult = index
		}
	}
	if reviewingResult < 0 {
		if review.Outcome != "none" || review.StepID != "" ||
			review.Summary != "" || review.ErrorReason != "" {
			return fmt.Errorf("model returned an unexpected observation review")
		}
		return nil
	}
	result := &results[reviewingResult]
	if review.StepID != result.StepID ||
		(review.Outcome != StepCompleted && review.Outcome != "error") ||
		len(review.Summary) == 0 ||
		len(review.Summary) > maxReviewSummary ||
		len(review.ErrorReason) > maxReviewSummary {
		return fmt.Errorf("model returned an invalid observation review")
	}
	if review.Outcome == StepCompleted &&
		((result.ExitCode != nil && *result.ExitCode != 0) ||
			result.ErrorReason != "") {
		return fmt.Errorf("model marked authoritative execution failure as completed")
	}
	status := StepCompleted
	if review.Outcome == "error" {
		status = StepFailed
	}
	result.Status = status
	result.Summary = review.Summary
	if review.ErrorReason != "" {
		result.ErrorReason = review.ErrorReason
	}
	for index := range steps {
		if steps[index].ID == result.StepID &&
			steps[index].Status == StepReviewing {
			steps[index].Status = status
			return nil
		}
	}
	return fmt.Errorf("reviewed step is unavailable")
}

func revisePendingSteps(
	steps []Step,
	drafts []PlannedStep,
	nextReference string,
) ([]Step, string, error) {
	if len(drafts) > maxPendingSteps {
		return nil, "", fmt.Errorf(
			"model returned more than %d pending steps", maxPendingSteps,
		)
	}
	history := make([]Step, 0, len(steps))
	currentPending := make(map[string]Step)
	known := make(map[string]Step)
	for _, step := range steps {
		known[step.ID] = step
		if step.Status == StepPending {
			currentPending[step.ID] = step
		} else {
			history = append(history, step)
		}
	}
	references := make(map[string]string, len(drafts))
	revised := make([]Step, len(drafts))
	for index, draft := range drafts {
		if err := validatePlannedStep(draft); err != nil {
			return nil, "", err
		}
		if _, exists := references[draft.ID]; exists {
			return nil, "", fmt.Errorf("model returned duplicate step references")
		}
		step, exists := currentPending[draft.ID]
		if !exists {
			if _, immutable := known[draft.ID]; immutable {
				return nil, "", fmt.Errorf("model attempted to modify immutable step history")
			}
			step = Step{ID: runtimeID("step"), Status: StepPending}
		}
		step.Title = draft.Title
		step.Objective = draft.Objective
		step.rootStepID = ""
		references[draft.ID] = step.ID
		revised[index] = step
	}
	all := make(map[string]*Step, len(history)+len(revised))
	for index := range history {
		all[history[index].ID] = &history[index]
	}
	for index := range revised {
		all[revised[index].ID] = &revised[index]
	}
	for index, draft := range drafts {
		parentID := draft.ParentStepID
		if mapped, exists := references[parentID]; exists {
			parentID = mapped
		}
		if parentID != "" {
			if _, exists := all[parentID]; !exists {
				return nil, "", fmt.Errorf("model returned an unknown parent step")
			}
		}
		if existing, exists := currentPending[draft.ID]; exists &&
			parentID != existing.ParentStepID {
			return nil, "", fmt.Errorf(
				"model changed an existing step relationship; use a new step for split or merge",
			)
		}
		revised[index].ParentStepID = parentID
	}
	visiting := make(map[string]bool)
	resolved := make(map[string]bool)
	var resolveRoot func(*Step) error
	resolveRoot = func(step *Step) error {
		if resolved[step.ID] {
			return nil
		}
		if visiting[step.ID] {
			return fmt.Errorf("model returned a cyclic step relationship")
		}
		visiting[step.ID] = true
		if step.ParentStepID == "" {
			step.rootStepID = step.ID
		} else {
			parent := all[step.ParentStepID]
			if parent == nil {
				return fmt.Errorf("model returned an unknown parent step")
			}
			if parent.rootStepID == "" {
				if err := resolveRoot(parent); err != nil {
					return err
				}
			}
			step.rootStepID = parent.rootStepID
		}
		visiting[step.ID] = false
		resolved[step.ID] = true
		return nil
	}
	for index := range revised {
		if err := resolveRoot(&revised[index]); err != nil {
			return nil, "", err
		}
	}
	nextStepID := ""
	if nextReference != "" {
		nextStepID = references[nextReference]
		if nextStepID == "" {
			return nil, "", fmt.Errorf("model selected an unknown next step")
		}
	}
	return append(history, revised...), nextStepID, nil
}

func validatePlannedStep(step PlannedStep) error {
	if len(step.ID) == 0 || len(step.ID) > maxStepReference ||
		len(step.ParentStepID) > maxStepReference {
		return fmt.Errorf("model returned an invalid step reference")
	}
	if len(step.Title) == 0 || len(step.Title) > maxStepTitle ||
		len(step.Objective) == 0 || len(step.Objective) > maxStepObjective {
		return fmt.Errorf("model returned an invalid pending step")
	}
	return nil
}

func latestObservationFingerprint(results []StepResult) string {
	if len(results) == 0 {
		return ""
	}
	result := results[len(results)-1]
	value := strings.Join([]string{
		result.Status, result.Output, result.Summary,
		result.ErrorReason, optionalResultExitCode(result.ExitCode),
	}, "\x00")
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func commandContextKey(rootID, command, observation string) string {
	value := strings.Join([]string{
		rootID, strings.TrimSpace(command), observation,
	}, "\x00")
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func optionalResultExitCode(value *int) string {
	if value == nil {
		return ""
	}
	return fmt.Sprintf("%d", *value)
}

func cloneSteps(steps []Step) []Step {
	result := make([]Step, len(steps))
	copy(result, steps)
	return result
}

func cloneResults(results []StepResult) []StepResult {
	result := make([]StepResult, len(results))
	copy(result, results)
	return result
}
