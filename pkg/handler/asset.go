package handler

import (
	"fmt"
	"strconv"

	"github.com/jumpserver/koko/pkg/common"
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
		"name":    {},
		"address": {},
		"ip":      {},
		//"platform": {},
		"org_name": {},
		"comment":  {},
	}
	return u.searchLocalFromFields(fields, searches...)
}

func (u *UserSelectHandler) displayAssetResult(searchHeader string) {
	term := u.h.term
	lang := i18n.NewLang(u.h.i18nLang)
	if len(u.currentResult) == 0 {
		noAssets := lang.T("No Assets")
		utils.IgnoreErrWriteString(term, utils.WrapperString(noAssets, utils.Red))
		utils.IgnoreErrWriteString(term, utils.CharNewLine)
		utils.IgnoreErrWriteString(term, utils.WrapperString(searchHeader, utils.Green))
		utils.IgnoreErrWriteString(term, utils.CharNewLine)
		return
	}
	u.displaySortedAssets(searchHeader)
}

func (u *UserSelectHandler) displaySortedAssets(searchHeader string) {
	lang := i18n.NewLang(u.h.i18nLang)
	term := u.h.term
	currentPage := u.CurrentPage()
	pageSize := u.PageSize()
	totalPage := u.TotalPage()
	totalCount := u.TotalCount()

	idLabel := lang.T("ID")
	hostLabel := lang.T("Hostname")
	ipLabel := lang.T("IP")
	platformLabel := lang.T("Platform")
	orgLabel := lang.T("Organization")
	commentLabel := lang.T("Comment")

	Labels := []string{idLabel, hostLabel, ipLabel, platformLabel, orgLabel, commentLabel}
	fields := []string{"ID", "Hostname", "IP", "Platform", "Organization", "Comment"}
	data := make([]map[string]string, len(u.currentResult))
	for i, j := range u.currentResult {
		row := make(map[string]string)
		row["ID"] = strconv.Itoa(i + 1)
		fieldMap := map[string]string{
			"name":     "Hostname",
			"address":  "IP",
			"platform": "Platform",
			"org_name": "Organization",
			"comment":  "Comment",
		}
		rowData := map[string]interface{}{
			"id":       j.ID,
			"name":     j.Name,
			"address":  j.Address,
			"platform": j.Platform.Name,
			"org_name": j.OrgName,
			"comment":  j.Comment,
		}

		row = convertMapItemToRow(rowData, fieldMap, row)
		row["Comment"] = joinMultiLineString(row["Comment"])
		data[i] = row
	}
	w, _ := term.GetSize()
	caption := fmt.Sprintf(lang.T("Page: %d, Count: %d, Total Page: %d, Total Count: %d"),
		currentPage, pageSize, totalPage, totalCount)

	caption = utils.WrapperString(caption, utils.Green)
	table := common.WrapperTable{
		Fields: fields,
		Labels: Labels,
		FieldsSize: map[string][3]int{
			"ID":           {0, 0, 5},
			"Hostname":     {0, 40, 0},
			"IP":           {0, 8, 40},
			"Platform":     {0, 8, 0},
			"Organization": {0, 8, 0},
			"Comment":      {0, 0, 0},
		},
		Data:        data,
		TotalSize:   w,
		Caption:     caption,
		TruncPolicy: common.TruncMiddle,
	}
	table.Initial()
	loginTip := lang.T("Enter ID number directly login the asset, multiple search use // + field, such as: //16")
	pageActionTip := lang.T("Page up: b	Page down: n")
	actionTip := fmt.Sprintf("%s %s", loginTip, pageActionTip)

	_, _ = term.Write([]byte(utils.CharClear))
	_, _ = term.Write([]byte(table.Display()))
	utils.IgnoreErrWriteString(term, utils.WrapperString(actionTip, utils.Green))
	utils.IgnoreErrWriteString(term, utils.CharNewLine)
	utils.IgnoreErrWriteString(term, utils.WrapperString(searchHeader, utils.Green))
	utils.IgnoreErrWriteString(term, utils.CharNewLine)
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
		AccountName:   selectedAccount.Username,
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
	proxyOpts = append(proxyOpts, proxy.ConnectDomain(&connectToken.Domain))
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
