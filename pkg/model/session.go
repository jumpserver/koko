package model

type Command struct {
	SessionId  string `json:"session"`
	OrgId      string `json:"org_id"`
	Input      string `json:"input"`
	Output     string `json:"output"`
	User       string `json:"user"`
	Server     string `json:"asset"`
	SystemUser string `json:"system_user"`
	Timestamp  int64  `json:"timestamp"`
}
