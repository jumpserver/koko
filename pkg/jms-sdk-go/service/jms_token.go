package service

import (
	"fmt"

	"github.com/jumpserver/koko/pkg/jms-sdk-go/model"
)

func (s *JMService) GetTokenAsset(token string) (tokenUser model.TokenUser, err error) {
	Url := fmt.Sprintf(TokenAssetURL, token)
	_, err = s.authClient.Get(Url, &tokenUser)
	return
}

func (s *JMService) GetConnectTokenInfo(tokenId string) (resp model.ConnectToken, err error) {
	data := map[string]string{
		"id": tokenId,
	}
	_, err = s.authClient.Post(ConnectTokenInfoURL, data, &resp)
	return
}

func (s *JMService) CreateSuperConnectToken(params *SuperConnectTokenReq) (resp model.ConnectTokenInfo, err error) {
	_, err = s.authClient.Post(SuperConnectTokenInfoURL, params, &resp)
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
}
