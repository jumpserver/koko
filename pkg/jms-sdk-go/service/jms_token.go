package service

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/jumpserver/koko/pkg/jms-sdk-go/model"
)

func (s *JMService) GetTokenAsset(token string) (tokenUser model.TokenUser, err error) {
	Url := fmt.Sprintf(TokenAssetURL, token)
	_, err = s.authClient.Get(Url, &tokenUser)
	return
}

func (s *JMService) GetConnectTokenAuth(token string) (resp TokenAuthInfoResponse, err error) {
	data := map[string]string{
		"token": token,
	}
	_, err = s.authClient.Post(TokenAuthInfoURL, data, &resp)
	return
}

func (s *JMService) RenewalToken(token string) (resp TokenRenewalResponse, err error) {
	data := map[string]string{
		"token": token,
	}
	_, err = s.authClient.Patch(TokenRenewalURL, data, &resp)
	return
}

func (s *JMService) GetConnectTokenInfo(token string) (resp ConnectToken, err error) {
	data := map[string]string{
		"token": token,
	}
	_, err = s.authClient.Post(ConnectTokenInfoURL, data, &resp)
	return
}

type TokenRenewalResponse struct {
	Ok  bool   `json:"ok"`
	Msg string `json:"msg"`
}

type TokenAuthInfoResponse struct {
	Info model.ConnectTokenInfo
	Err  []string
}

/*
	接口返回可能是一个['Token not found']
*/

func (t *TokenAuthInfoResponse) UnmarshalJSON(p []byte) error {
	if index := bytes.IndexByte(p, '['); index == 0 {
		return json.Unmarshal(p, &t.Err)
	}
	return json.Unmarshal(p, &t.Info)
}

type ConnectToken struct {
	Id       string          `json:"id"`
	User     model.User      `json:"user"`
	Value    string          `json:"value"`
	Account  model.Account   `json:"account"`
	Actions  []model.Action  `json:"actions"`
	Asset    model.Asset     `json:"asset"`
	Protocol string          `json:"protocol"`
	Domain   model.Domain    `json:"domain"`
	Gateway  []model.Gateway `json:"gateway"`
	ExpireAt int64           `json:"expire_at"`
	OrgId    string          `json:"org_id"`
	OrgName  string          `json:"org_name"`
	Platform model.Platform  `json:"platform"`

	Code   string `json:"code"`
	Detail string `json:"detail"`
}

func (c *ConnectToken) Permission() model.Permission {
	var permission model.Permission
	permission.Actions = make([]string, 0, len(c.Actions))
	for i := range c.Actions {
		permission.Actions = append(permission.Actions, c.Actions[i].Value)
	}
	return permission
}
