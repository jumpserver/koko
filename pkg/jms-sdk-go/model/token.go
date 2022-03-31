package model

type ConnectTokenInfo struct {
	Id          string       `json:"id"`
	Secret      string       `json:"secret"`
	TypeName    ConnectType  `json:"type"`
	User        *User        `json:"user"`
	Actions     []string     `json:"actions,omitempty"`
	Application *Application `json:"application,omitempty"`
	Asset       *Asset       `json:"asset,omitempty"`
	ExpiredAt   int64        `json:"expired_at"`
	Gateway     Gateway      `json:"gateway,omitempty"`
	Domain      *Domain      `json:"domain"`

	CmdFilterRules FilterRules `json:"cmd_filter_rules,omitempty"`

	SystemUserAuthInfo *SystemUserAuthInfo `json:"system_user"`
}
