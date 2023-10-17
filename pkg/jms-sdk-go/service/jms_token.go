package service

import (
	"github.com/jumpserver/koko/pkg/jms-sdk-go/model"
)

func (s *JMService) GetConnectTokenInfo(tokenId string) (resp model.ConnectToken, err error) {
	data := map[string]string{
		"id": tokenId,
	}
	_, err = s.authClient.Post(SuperConnectTokenSecretURL, data, &resp)
	return
}

func (s *JMService) CreateSuperConnectToken(data *SuperConnectTokenReq) (resp model.ConnectTokenInfo, err error) {
	_, err = s.authClient.Post(SuperConnectTokenInfoURL, data, &resp, data.Params)
	return
}

type SuperConnectTokenReq struct {
	UserId        string `json:"user"`
	AssetId       string `json:"asset"`
	Account       string `json:"account"`
	Protocol      string `json:"protocol"`
	ConnectMethod string `json:"connect_method"`
	InputUsername string `json:"input_username"`
	InputSecret   string `json:"input_secret"`
	RemoteAddr    string `json:"remote_addr"`

	Params map[string]string `json:"-"`
}
