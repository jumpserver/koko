package model

import "github.com/jumpserver/koko/pkg/jms-sdk-go/common"

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

	CommandFilterACLs []CommandACL `json:"command_filter_acls"`

	Code   string `json:"code"`
	Detail string `json:"detail"`
}

func (c *ConnectToken) CreateSession(addr string,
	loginFrom, SessionType LabelFiled) Session {
	return Session{
		User:      c.User.String(),
		Asset:     c.Asset.String(),
		Account:   c.Account.String(),
		Protocol:  c.Protocol,
		OrgID:     c.OrgId,
		UserID:    c.User.ID,
		AssetID:   c.Asset.ID,
		DateStart: common.NewNowUTCTime(),

		RemoteAddr: addr,
		LoginFrom:  loginFrom,
		Type:       SessionType,
	}
}

type ConnectTokenInfo struct {
	ID          string `json:"id"`
	Value       string `json:"value"`
	ExpireTime  int    `json:"expire_time"`
	AccountName string `json:"account_name"`
	Protocol    string `json:"protocol"`
}
