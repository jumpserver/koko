package service

import (
	"fmt"

	"github.com/jumpserver/koko/pkg/jms-sdk-go/model"
)

func (s *JMService) GetAssetPlatform(assetId string) (platform model.Platform, err error) {
	url := fmt.Sprintf(AssetPlatFormURL, assetId)
	_, err = s.authClient.Get(url, &platform)
	return
}

func (s *JMService) GetDomainGateways(domainId string) (domain model.Domain, err error) {
	Url := fmt.Sprintf(DomainDetailWithGateways, domainId)
	_, err = s.authClient.Get(Url, &domain)
	return
}

func (s *JMService) GetAccountSecretById(accountId string) (res model.AccountDetail, err error) {
	url := fmt.Sprintf(AccountSecretURL, accountId)
	_, err = s.authClient.Get(url, &res)
	return
}

func (s *JMService) GetAccountChat() (res model.AccountChatDetail, err error) {
	url := fmt.Sprintf(AccountChatURL)
	_, err = s.authClient.Get(url, &res)
	return
}
