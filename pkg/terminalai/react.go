package terminalai

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
)

const (
	maxReActRounds        = 20
	maxStepAttempts       = 3
	maxSameCommandContext = 2
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
	transition.nextStepID = decision.NextStepID
	switch decision.Kind {
	case ReActExecute:
		if decision.Proposal == nil || decision.Summary != "" {
			return transition, fmt.Errorf("model returned an invalid execute action")
		}
		if transition.nextStepID == "" {
			return transition, fmt.Errorf("model execute action has no next step")
		}
		if decision.Observation.Outcome == ReActContinue &&
			decision.Observation.StepID != transition.nextStepID {
			return transition, fmt.Errorf(
				"model must continue the same logical task",
			)
		}
		var err error
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
			if transition.steps[index].Status == StepPending ||
				transition.steps[index].Status == StepInProgress {
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
		if candidate.ID == stepID &&
			(candidate.Status == StepPending ||
				candidate.Status == StepInProgress) {
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
			(p.steps[index].Status == StepPending ||
				p.steps[index].Status == StepInProgress) {
			p.steps[index].Status = StepInProgress
			p.commandRepeats[transition.commandContextKey]++
			return nil
		}
	}
	return fmt.Errorf("next execute step is unavailable")
}

func (p *reactPlan) recordExecution(
	executionID string,
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
			ID: executionID, StepID: stepID, Command: proposal.Command,
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
	executionID string,
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
		ID: executionID, StepID: stepID, Command: proposal.Command,
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

func (p *reactPlan) interrupt(reason string) {
	for index := range p.steps {
		switch p.steps[index].Status {
		case StepPending:
			p.steps[index].Status = StepSkipped
		case StepInProgress, StepReviewing:
			p.steps[index].Status = StepInterrupted
		}
	}
	for index := range p.results {
		if p.results[index].Status != StepReviewing {
			continue
		}
		p.results[index].Status = StepInterrupted
		if p.results[index].Summary == "" {
			p.results[index].Summary = reason
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
		(review.Outcome != StepCompleted && review.Outcome != "error" &&
			review.Outcome != ReActContinue) ||
		len(review.Summary) == 0 ||
		len(review.Summary) > maxReviewSummary ||
		len(review.ErrorReason) > maxReviewSummary {
		return fmt.Errorf("model returned an invalid observation review")
	}
	commandFailed := result.ExitCode != nil && *result.ExitCode != 0 ||
		result.ErrorReason != "" || review.ErrorReason != ""
	if review.Outcome == StepCompleted &&
		commandFailed {
		return fmt.Errorf("model marked authoritative execution failure as completed")
	}
	resultStatus := StepCompleted
	if commandFailed || review.Outcome == "error" {
		resultStatus = StepFailed
	}
	result.Status = resultStatus
	result.Summary = review.Summary
	if review.ErrorReason != "" {
		result.ErrorReason = review.ErrorReason
	}
	for index := range steps {
		if steps[index].ID == result.StepID &&
			steps[index].Status == StepReviewing {
			switch review.Outcome {
			case StepCompleted:
				steps[index].Status = StepCompleted
			case "error":
				steps[index].Status = StepFailed
			case ReActContinue:
				steps[index].Status = StepInProgress
			}
			return nil
		}
	}
	return fmt.Errorf("reviewed step is unavailable")
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
