package model

import (
	"github.com/jumpserver/koko/pkg/jms-sdk-go/common"
)

type ConnectToken struct {
	Id       string     `json:"id"`
	User     User       `json:"user"`
	Value    string     `json:"value"`
	Account  Account    `json:"account"`
	Actions  Actions    `json:"actions"`
	Asset    Asset      `json:"asset"`
	Protocol string     `json:"protocol"`
	Domain   *Domain    `json:"domain"`
	Gateway  *Gateway   `json:"gateway"`
	ExpireAt ExpireInfo `json:"expire_at"`
	OrgId    string     `json:"org_id"`
	OrgName  string     `json:"org_name"`
	Platform Platform   `json:"platform"`

	ConnectOptions ConnectOptions `json:"connect_options"`

	CommandFilterACLs []CommandACL `json:"command_filter_acls"`

	Ticket     *ObjectId   `json:"from_ticket,omitempty"`
	TicketInfo interface{} `json:"from_ticket_info,omitempty"`

	Code   string `json:"code"`
	Detail string `json:"detail"`
}

func (c *ConnectToken) CreateSession(addr string,
	loginFrom, SessionType LabelField) Session {
	return Session{
		User:      c.User.String(),
		Asset:     c.Asset.String(),
		Account:   c.Account.String(),
		Protocol:  c.Protocol,
		OrgID:     c.OrgId,
		UserID:    c.User.ID,
		AssetID:   c.Asset.ID,
		AccountID: c.Account.ID,
		DateStart: common.NewNowUTCTime(),

		RemoteAddr: addr,
		LoginFrom:  loginFrom,
		Type:       SessionType,
		ErrReason:  LabelField(SessionReplayErrUnsupported),
	}
}

type ConnectTokenInfo struct {
	ID          string `json:"id"`
	Value       string `json:"value"`
	ExpireTime  int    `json:"expire_time"`
	AccountName string `json:"account_name"`
	Protocol    string `json:"protocol"`

	Ticket     *ObjectId  `json:"from_ticket,omitempty"`
	TicketInfo TicketInfo `json:"from_ticket_info,omitempty"`

	Code   string `json:"code,omitempty"`
	Detail string `json:"detail,omitempty"`
}

const (
	ACLReview = "acl_review"
	ACLReject = "acl_reject"
)

type ConnectOptions struct {
	Charset          *string `json:"charset,omitempty"`
	DisableAutoHash  *bool   `json:"disableautohash,omitempty"`
	BackspaceAsCtrlH *bool   `json:"backspaceAsCtrlH,omitempty"`

	FilenameConflictResolution string `json:"file_name_conflict_resolution,omitempty"`
	TerminalThemeName          string `json:"terminal_theme_name,omitempty"`
}
