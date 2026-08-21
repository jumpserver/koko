package terminalai

import "encoding/json"

const (
	ModeAuto       = "auto"
	ModePTYOnly    = "pty_only"
	ModeBackground = "background_only"

	ExecutionPTY        = "pty"
	ExecutionBackground = "background_exec"

	ReActExecute  = "execute"
	ReActFinish   = "finish"
	ReActContinue = "continue"

	StepPending     = "pending"
	StepInProgress  = "in_progress"
	StepReviewing   = "reviewing"
	StepCompleted   = "completed"
	StepFailed      = "failed"
	StepInterrupted = "interrupted"
	StepRejected    = "rejected"
	StepSkipped     = "skipped"
)

type ChatMessage struct {
	ID       string         `json:"id"`
	Role     string         `json:"role"`
	Metadata map[string]any `json:"metadata,omitempty"`
	Parts    []ChatPart     `json:"parts"`
}

type ChatPart struct {
	Type  string `json:"type"`
	Text  string `json:"text,omitempty"`
	State string `json:"state,omitempty"`
	Data  any    `json:"data,omitempty"`
}

type Step struct {
	ID           string `json:"id"`
	ParentStepID string `json:"parentStepId,omitempty"`
	Title        string `json:"title"`
	Objective    string `json:"objective"`
	Status       string `json:"status"`
	rootStepID   string
}

type Decision struct {
	Kind           string           `json:"kind"`
	Answer         string           `json:"answer"`
	Summary        string           `json:"summary"`
	ThoughtSummary string           `json:"thoughtSummary"`
	Steps          []Step           `json:"steps"`
	Proposal       *CommandProposal `json:"proposal"`
}

type CommandProposal struct {
	Command             string              `json:"command"`
	Rationale           string              `json:"rationale"`
	RiskLevel           int                 `json:"riskLevel"`
	RiskReason          string              `json:"riskReason"`
	Execution           string              `json:"execution"`
	ExecutionCause      string              `json:"executionReason"`
	CommandACL          *CommandACLDecision `json:"-"`
	BackgroundEligible  bool                `json:"-"`
	ApprovalRequired    bool                `json:"-"`
	MaxExecutionSeconds int                 `json:"-"`
	RuleMatches         []RuleMatch         `json:"-"`
	DeniedByRules       []RuleMatch         `json:"-"`
	RulePolicy          RuleCommandPolicy   `json:"-"`
}

type ObservationReview struct {
	StepID      string `json:"stepId"`
	Outcome     string `json:"outcome"`
	Summary     string `json:"summary"`
	ErrorReason string `json:"errorReason"`
}

type ReActDecision struct {
	Kind           string            `json:"kind"`
	ThoughtSummary string            `json:"thoughtSummary"`
	Observation    ObservationReview `json:"observation"`
	NextStepID     string            `json:"nextStepId"`
	Proposal       *CommandProposal  `json:"proposal"`
	Summary        string            `json:"summary"`
}

type StepResult struct {
	ID              string `json:"executionId"`
	StepID          string `json:"stepId"`
	Command         string `json:"command"`
	Output          string `json:"output"`
	OutputTruncated bool   `json:"outputTruncated,omitempty"`
	Status          string `json:"status"`
	Summary         string `json:"summary"`
	ErrorReason     string `json:"errorReason,omitempty"`
	Execution       string `json:"execution"`
	ExitCode        *int   `json:"exitCode,omitempty"`
}

type CommandACLDecision struct {
	Action    string   `json:"action"`
	ACLID     string   `json:"aclId,omitempty"`
	ItemID    string   `json:"itemId,omitempty"`
	Name      string   `json:"name,omitempty"`
	Matched   string   `json:"matched,omitempty"`
	DetailURL string   `json:"detailUrl,omitempty"`
	Reviewers []string `json:"reviewers,omitempty"`
	Processor string   `json:"processor,omitempty"`
	Reviewed  bool     `json:"reviewed,omitempty"`
}

type approvalDecision struct {
	ID        string `json:"id"`
	Digest    string `json:"digest"`
	Approved  bool   `json:"approved"`
	Execution string `json:"execution,omitempty"`
}

type policyUpdate struct {
	ApprovalThreshold int    `json:"approvalThreshold,omitempty"`
	ExecutionMode     string `json:"executionMode,omitempty"`
}

func DecodeChatMessage(value string) (ChatMessage, error) {
	var message ChatMessage
	err := json.Unmarshal([]byte(value), &message)
	return message, err
}
