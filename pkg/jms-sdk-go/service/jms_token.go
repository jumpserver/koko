package service

import (
	"fmt"
	"strings"

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
	ak := s.opt.accessKey
	apiClient := s.authClient.Clone()
	if s.opt.sign != nil {
		apiClient.SetAuthSign(s.opt.sign)
	}
	apiClient.SetHeader(orgHeaderKey, orgHeaderValue)
	// 移除 Secret 中的 "-", 保证长度为 32
	secretKey := strings.ReplaceAll(ak.Secret, "-", "")
	encryptKey, err1 := GenerateEncryptKey(secretKey)
	if err != nil {
		return resp, err1
	}
	signKey := fmt.Sprintf("%s:%s", ak.ID, encryptKey)
	apiClient.SetHeader(svcHeader, fmt.Sprintf("Sign %s", signKey))
	_, err = apiClient.Post(SuperConnectTokenInfoURL, data, &resp, data.Params)
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
