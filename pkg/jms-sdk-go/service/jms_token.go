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

func (s *JMService) GetConnectTokenInfo(tokenId string) (resp ConnectToken, err error) {
	data := map[string]string{
		"id": tokenId,
	}
	_, err = s.authClient.Post(ConnectTokenInfoURL, data, &resp)
	return
}

func (s *JMService) CreateSuperConnectToken(params *SuperConnectTokenReq) (resp ConnectTokenInfo, err error) {
	_, err = s.authClient.Post(SuperConnectTokenInfoURL, params, &resp)
	return
}

/*
	data = {
	     asset: asset.id,
	     account_name: accountName,
	     protocol: protocol.name,
	     input_username: manualAuthInfo.username,
	     input_secret: manualAuthInfo.secret,
	     connect_method: ssh,
	   };
*/
type SuperConnectTokenReq struct {
	UserId        string `json:"user"`
	AssetId       string `json:"asset"`
	AccountName   string `json:"account_name"`
	Protocol      string `json:"protocol"`
	ConnectMethod string `json:"connect_method"`
	InputUsername string `json:"input_username"`
	InputSecret   string `json:"input_secret"`
}

/*
{'account_name': 'root',
 'actions': 31,
 'asset': 'b1fff872-f58a-4fd3-9e26-7f3c6c7d74f0',
 'asset_display': '172(172.16.10.122)',
 'connect_method': 'ssh',
 'created_by': '192.168.200.101',
 'date_created': '2022/12/01 11:10:16 +0800',
 'date_expired': '2022/12/01 11:15:16 +0800',
 'date_updated': '2022/12/01 11:10:16 +0800',
 'expire_time': 299,
 'id': 'c700e7f7-7e4d-4f33-b891-f9a69595647c',
 'input_secret': '',
 'input_username': '',
 'org_id': '00000000-0000-0000-0000-000000000002',
 'org_name': 'Default',
 'protocol': 'ssh',
 'updated_by': None,
 'user': '3997a9e8-e78a-43ba-a67a-d536198b2c69',
 'user_display': 'Administrator(admin)',
 'value': 'yViyF4GuoqWDxKFe'}
*/

type ConnectTokenInfo struct {
	ID          string `json:"id"`
	Value       string `json:"value"`
	ExpireTime  int    `json:"expire_time"`
	AccountName string `json:"account_name"`
	Protocol    string `json:"protocol"`
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
