package model

type ConnectToken struct {
	Id       string   `json:"id"`
	User     User     `json:"user"`
	Value    string   `json:"value"`
	Account  Account  `json:"account"`
	Actions  Actions  `json:"actions"`
	Asset    Asset    `json:"asset"`
	Protocol string   `json:"protocol"`
	Domain   Domain   `json:"domain"`
	Gateway  Gateway  `json:"gateway"`
	ExpireAt int64    `json:"expire_at"`
	OrgId    string   `json:"org_id"`
	OrgName  string   `json:"org_name"`
	Platform Platform `json:"platform"`

	Code   string `json:"code"`
	Detail string `json:"detail"`
}

func (c *ConnectToken) Permission() Permission {
	var permission Permission
	permission.Actions = make([]string, 0, len(c.Actions))
	for i := range c.Actions {
		permission.Actions = append(permission.Actions, c.Actions[i].Value)
	}
	return permission
}

type ConnectTokenInfo struct {
	ID          string `json:"id"`
	Value       string `json:"value"`
	ExpireTime  int    `json:"expire_time"`
	AccountName string `json:"account_name"`
	Protocol    string `json:"protocol"`
}
