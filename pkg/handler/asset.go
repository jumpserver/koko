package handler

import (
	"strconv"
	"strings"

	"github.com/jumpserver/koko/pkg/i18n"
	"github.com/jumpserver/koko/pkg/jms-sdk-go/model"
	"github.com/jumpserver/koko/pkg/jms-sdk-go/service"
	"github.com/jumpserver/koko/pkg/logger"
	"github.com/jumpserver/koko/pkg/proxy"
	"github.com/jumpserver/koko/pkg/utils"
)

func (u *UserSelectHandler) retrieveRemoteAsset(reqParam model.PaginationParam) []model.Asset {
	res, err := u.h.jmsService.GetUserPermsAssets(u.user.ID, reqParam)
	if err != nil {
		logger.Errorf("Get user perm assets failed: %s", err.Error())
	}
	return u.updateRemotePageData(reqParam, res)
}

func (u *UserSelectHandler) searchLocalAsset(searches ...string) []model.Asset {
	fields := map[string]struct{}{
		"name":     {},
		"address":  {},
		"ip":       {},
		"platform": {},
		"org_name": {},
		"comment":  {},
	}
	return u.searchLocalFromFields(fields, searches...)
}

func (u *UserSelectHandler) displayAssetResult(searchHeader string) {
	lang := i18n.NewLang(u.h.i18nLang)
	if len(u.currentResult) == 0 {
		noAssets := lang.T("No Assets")
		u.displayNoResultMsg(searchHeader, noAssets)
		return
	}
	u.displayAssets(searchHeader)
}

func (u *UserSelectHandler) displayAssets(searchHeader string) {
	lang := i18n.NewLang(u.h.i18nLang)
	idLabel := lang.T("ID")
	hostLabel := lang.T("Hostname")
	ipLabel := lang.T("IP")
	protocolsLabel := lang.T("Protocols")
	platformLabel := lang.T("Platform")
	orgLabel := lang.T("Organization")
	commentLabel := lang.T("Comment")

	Labels := []string{idLabel, hostLabel, ipLabel, protocolsLabel, platformLabel, orgLabel, commentLabel}
	fields := []string{"ID", "Hostname", "IP", "Protocols", "Platform", "Organization", "Comment"}
	fieldsSize := map[string][3]int{
		"ID":           {0, 0, 5},
		"Hostname":     {0, 40, 0},
		"IP":           {0, 8, 40},
		"Protocols":    {0, 8, 0},
		"Platform":     {0, 8, 0},
		"Organization": {0, 8, 0},
		"Comment":      {0, 0, 0},
	}
	generateRowFunc := func(i int, item *model.Asset) map[string]string {
		row := make(map[string]string)
		row["ID"] = strconv.Itoa(i + 1)
		row["Hostname"] = item.Name
		row["IP"] = item.Address
		row["Protocols"] = strings.Join(item.SupportProtocols(), "|")
		row["Platform"] = item.Platform.Name
		row["Organization"] = item.OrgName
		row["Comment"] = joinMultiLineString(item.Comment)
		return row
	}
	assetDisplay := lang.T("the asset")
	data := make([]map[string]string, len(u.currentResult))
	for i := range u.currentResult {
		data[i] = generateRowFunc(i, &u.currentResult[i])
	}
	u.displayResult(searchHeader, assetDisplay,
		Labels, fields, fieldsSize, generateRowFunc)

}

func (u *UserSelectHandler) proxyAsset(asset model.Asset) {
	accounts, err := u.h.jmsService.GetAccountsByUserIdAndAssetId(u.user.ID, asset.ID)
	if err != nil {
		logger.Errorf("Get asset accounts err: %s", err)
		return
	}
	protocol, ok := u.h.chooseAssetProtocol(asset.SupportProtocols())
	if !ok {
		logger.Info("not select protocol")
		return
	}
	selectedAccount, ok := u.h.chooseAccount(accounts)
	if !ok {
		return
	}
	i18nLang := u.h.i18nLang
	req := service.SuperConnectTokenReq{
		UserId:        u.user.ID,
		AssetId:       asset.ID,
		Account:       selectedAccount.Name,
		Protocol:      protocol,
		ConnectMethod: "ssh",
	}
	res, err := u.h.jmsService.CreateSuperConnectToken(&req)
	if err != nil {
		logger.Errorf("Create super connect token err: %s", err)
		utils.IgnoreErrWriteString(u.h.term, "create connect token err")
		return
	}
	connectToken, err := u.h.jmsService.GetConnectTokenInfo(res.ID)
	if err != nil {
		logger.Errorf("connect token err: %s", err)
		utils.IgnoreErrWriteString(u.h.term, "get connect token err")
		return
	}
	user := u.h.user
	proxyOpts := make([]proxy.ConnectionOption, 0, 10)
	proxyOpts = append(proxyOpts, proxy.ConnectProtocol(protocol))
	proxyOpts = append(proxyOpts, proxy.ConnectUser(user))
	proxyOpts = append(proxyOpts, proxy.ConnectAsset(&connectToken.Asset))
	proxyOpts = append(proxyOpts, proxy.ConnectAccount(&connectToken.Account))
	proxyOpts = append(proxyOpts, proxy.ConnectActions(connectToken.Actions))
	proxyOpts = append(proxyOpts, proxy.ConnectExpired(connectToken.ExpireAt))
	proxyOpts = append(proxyOpts, proxy.ConnectDomain(connectToken.Domain))
	proxyOpts = append(proxyOpts, proxy.ConnectPlatform(&connectToken.Platform))
	proxyOpts = append(proxyOpts, proxy.ConnectGateway(connectToken.Gateway))
	proxyOpts = append(proxyOpts, proxy.ConnectI18nLang(i18nLang))
	srv, err := proxy.NewServer(u.h.sess, u.h.jmsService, proxyOpts...)
	if err != nil {
		logger.Errorf("create proxy server err: %s", err)
		return
	}
	srv.Proxy()
}
