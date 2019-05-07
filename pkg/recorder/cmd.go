package recorder

import (
	"time"
)

type CommandRecorder struct {
	SessionID string
	StartTime time.Time
}

type Command struct {
	SessionId  string    `json:"session"`
	OrgId      string    `json:"org_id"`
	Input      string    `json:"input"`
	Output     string    `json:"output"`
	User       string    `json:"user"`
	Server     string    `json:"asset"`
	SystemUser string    `json:"system_user"`
	Timestamp  time.Time `json:"timestamp"`
}

func (c *CommandRecorder) Record(cmd *Command) {

}
