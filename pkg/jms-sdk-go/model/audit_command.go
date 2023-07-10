package model

import "time"

type Command struct {
	SessionID string `json:"session"`
	OrgID     string `json:"org_id"`
	Input     string `json:"input"`
	Output    string `json:"output"`
	User      string `json:"user"`
	Server    string `json:"asset"`
	Account   string `json:"account"`
	Timestamp int64  `json:"timestamp"`
	RiskLevel int64  `json:"risk_level"`

	CmdFilterAclId string `json:"cmd_filter_acl"`
	CmdGroupId     string `json:"cmd_group"`

	DateCreated time.Time `json:"@timestamp"`
}

const (
	NormalLevel  = 0
	WarningLevel = 4
	RejectLevel  = 5
	ReviewReject = 6
	ReviewAccept = 7
	ReviewCancel = 8
)
