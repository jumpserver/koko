package auth

import (
	"errors"
	"fmt"
	"net"

	"github.com/gliderlabs/ssh"

	"github.com/jumpserver-dev/sdk-go/common"
	"github.com/jumpserver-dev/sdk-go/model"
	"github.com/jumpserver-dev/sdk-go/service"
	"github.com/jumpserver/koko/pkg/i18n"
	"github.com/jumpserver/koko/pkg/logger"
)

func GetMatchedAssetsByDirectReq(jmsService *service.JMService, user *model.User, req *DirectLoginAssetReq) ([]model.PermAsset, error) {
	var getUserPermAssets func() ([]model.PermAsset, error)
	if common.IsUUID(req.AssetTarget) {
		getUserPermAssets = func() ([]model.PermAsset, error) {
			return jmsService.GetUserPermAssetsById(user.ID, req.AssetTarget)
		}
	} else {
		getUserPermAssets = func() ([]model.PermAsset, error) {
			return jmsService.GetUserPermAssetsByIP(user.ID, req.AssetTarget)
		}
	}
	i18nLang := i18n.NewLang(user.Language)
	assets, err := getUserPermAssets()
	if err != nil {
		logger.Errorf("Get user %s perm asset failed: %s", user.String(), err)
		return nil, fmt.Errorf("match asset failed: %s", i18nLang.T("Core API failed"))
	}
	if len(assets) == 0 {
		logger.Infof("User %s no perm for asset %s", user.String(), req.AssetTarget)
		return nil, fmt.Errorf("match asset failed: %s", i18nLang.T("No found asset"))
	}
	return assets, nil
}

func GetMatchedAccounts(req *DirectLoginAssetReq, permAssetDetail model.PermAssetDetail) ([]model.PermAccount, error) {
	matched := make([]model.PermAccount, 0, len(permAssetDetail.PermedAccounts))
	for i := range permAssetDetail.PermedAccounts {
		account := permAssetDetail.PermedAccounts[i]
		if account.Username == req.AccountUsername {
			matched = append(matched, account)
		}
	}
	return matched, nil
}

func BuildDirectConnectToken(ctx ssh.Context, jmsService *service.JMService, user *model.User,
	req *DirectLoginAssetReq) (*model.ConnectToken, error) {
	if req.IsToken() {
		return req.ConnectToken, nil
	}
	selectedAssets, err := GetMatchedAssetsByDirectReq(jmsService, user, req)
	if err != nil {
		return nil, err
	}
	i18nLang := i18n.NewLang(user.Language)
	if len(selectedAssets) != 1 {
		msg := fmt.Sprintf(i18nLang.T("Must be unique asset for %s"), req.AssetTarget)
		return nil, errors.New(msg)
	}
	permAssetDetail, err := jmsService.GetUserPermAssetDetailById(user.ID, selectedAssets[0].ID)
	if err != nil {
		msg := fmt.Sprintf(i18nLang.T("Must be unique asset for %s"), req.AssetTarget)
		logger.Errorf("Get permAssetDetail failed: %s", err)
		return nil, errors.New(msg)
	}
	if !permAssetDetail.SupportProtocol(req.Protocol) {
		msg := fmt.Sprintf("not %s asset connection", req.Protocol)
		logger.Errorf("Direct Request %s failed: %s", req.Protocol, msg)
		return nil, errors.New(msg)
	}

	selectAccounts, err := GetMatchedAccounts(req, permAssetDetail)
	if err != nil {
		return nil, err
	}
	if len(selectAccounts) != 1 {
		msg := fmt.Sprintf(i18nLang.T("Must be unique account for %s"), req.AccountUsername)
		logger.Error(msg)
		return nil, errors.New(msg)
	}
	selectAccount := selectAccounts[0]
	switch selectAccount.Username {
	case model.InputUser, model.DynamicUser, model.ANONUser:
		msg := fmt.Sprintf(i18nLang.T("Must be auto login account for %s"), req.AccountUsername)
		logger.Error(msg)
		return nil, errors.New(msg)
	}
	remoteAddr, _, _ := net.SplitHostPort(ctx.RemoteAddr().String())
	sessReq := &service.SuperConnectTokenReq{
		UserId:        user.ID,
		AssetId:       permAssetDetail.ID,
		Account:       selectAccount.Alias,
		Protocol:      req.Protocol,
		ConnectMethod: req.Protocol,
		RemoteAddr:    remoteAddr,
	}
	tokenInfo, err := jmsService.CreateSuperConnectToken(sessReq)
	if err != nil {
		msg := err.Error()
		if tokenInfo.Detail != "" {
			msg = tokenInfo.Detail
		}
		logger.Errorf("Create super connect token failed: %s", msg)
		return nil, err
	}
	connectToken, err := jmsService.GetConnectTokenInfo(tokenInfo.ID, true)
	if err != nil {
		logger.Errorf("Create super connect token err: %s", err)
		return nil, err
	}
	return &connectToken, nil
}
