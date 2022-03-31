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
