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
	_, err = s.authClient.Post(SuperConnectTokenSecretURL, data, &resp)
	return
}

func (s *JMService) CreateSuperConnectToken(data *SuperConnectTokenReq) (resp model.ConnectTokenInfo, err error) {
	_, err = s.authClient.Post(SuperConnectTokenInfoURL, data, &resp, data.Params)
	return
}

func (s *JMService) CreateConnectTokenAndGetAuthInfo(params *SuperConnectTokenReq) (model.ConnectToken, error) {
	tokenInfo, err := s.CreateSuperConnectToken(params)
	if err != nil {
		return model.ConnectToken{}, err
	}
	connectToken, err := s.GetConnectTokenInfo(tokenInfo.ID)
	if err != nil {
		return model.ConnectToken{}, err
	}
	return connectToken, nil
}

type SuperConnectTokenReq struct {
	UserId        string `json:"user"`
	AssetId       string `json:"asset"`
	Account       string `json:"account"`
	Protocol      string `json:"protocol"`
	ConnectMethod string `json:"connect_method"`
	InputUsername string `json:"input_username"`
	InputSecret   string `json:"input_secret"`

	Params map[string]string `json:"-"`
}
