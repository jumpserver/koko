package handler

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/jumpserver/koko/pkg/i18n"
	"github.com/jumpserver/koko/pkg/jms-sdk-go/model"
	"github.com/jumpserver/koko/pkg/jms-sdk-go/service"
	"github.com/jumpserver/koko/pkg/logger"
	"github.com/jumpserver/koko/pkg/proxy"
	"github.com/jumpserver/koko/pkg/srvconn"
	"github.com/jumpserver/koko/pkg/utils"
)

func (u *UserSelectHandler) retrieveRemoteAsset(reqParam model.PaginationParam) []model.PermAsset {
	res, err := u.h.jmsService.GetUserPermsAssets(u.user.ID, reqParam)
	if err != nil {
		logger.Errorf("Get user perm assets failed: %s", err.Error())
	}
	return u.updateRemotePageData(reqParam, res)
}

func (u *UserSelectHandler) searchLocalAsset(searches ...string) []model.PermAsset {
	allFields := []string{"name", "address", "platform", "comment"}
	fields := make(map[string]struct{}, len(allFields))
	for i := range allFields {
		if u.isHiddenField(allFields[i]) {
			continue
		}
		fields[allFields[i]] = struct{}{}
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

const maxFieldSize = 80 // 仅仅是限制字段显示长度最大为 80

func (u *UserSelectHandler) displayAssets(searchHeader string) {
	currentResult := u.currentResult
	lang := i18n.NewLang(u.h.i18nLang)
	idLabel := lang.T("ID")
	nameLabel := lang.T("Name")
	addressLabel := lang.T("Address")
	platformLabel := lang.T("Platform")
	orgLabel := lang.T("Organization")
	commentLabel := lang.T("Comment")
	idFieldSize := len(idLabel)
	nameFieldSize := len(nameLabel)
	addressFieldSize := len(addressLabel)
	platformFieldSize := len(platformLabel)
	organizationFieldSize := len(orgLabel)
	commentFieldSize := len(commentLabel)
	data := make([]map[string]string, len(currentResult))
	for i := range currentResult {
		item := &u.currentResult[i]
		row := make(map[string]string)
		idNumber := strconv.Itoa(i + 1)
		row["ID"] = idNumber
		row["Name"] = strings.ReplaceAll(item.Name, " ", "_") // 多个空格可能会导致换行，所以全部替换成下划线
		row["Address"] = item.Address
		row["Platform"] = item.Platform.Name
		row["Organization"] = item.OrgName
		row["Comment"] = joinMultiLineString(item.Comment)
		data[i] = row
		if idFieldSize < len(idNumber) {
			idFieldSize = len(idNumber)
		}
		if len(item.Name) > nameFieldSize {
			nameFieldSize = len(item.Name)
		}
		if len(item.Address) > addressFieldSize {
			addressFieldSize = len(item.Address)
		}
	}
	if nameFieldSize > maxFieldSize {
		nameFieldSize = maxFieldSize
	}
	if addressFieldSize > maxFieldSize {
		addressFieldSize = maxFieldSize
	}

	allFieldsSize := map[string][3]int{
		"ID":           {idFieldSize, 0, 0},
		"Name":         {nameFieldSize, 0, 0},
		"Address":      {addressFieldSize, 0, 0},
		"Platform":     {0, platformFieldSize, 0},
		"Organization": {0, organizationFieldSize, 0},
		"Comment":      {0, commentFieldSize, 0},
	}
	allLabels := []string{idLabel, nameLabel, addressLabel, platformLabel, orgLabel, commentLabel}
	allFields := []string{"ID", "Name", "Address", "Platform", "Organization", "Comment"}
	labels := make([]string, 0, len(allLabels))
	fields := make([]string, 0, len(allFields))
	for i := range allFields {
		if u.isHiddenField(allFields[i]) {
			continue
		}
		labels = append(labels, allLabels[i])
		fields = append(fields, allFields[i])
	}
	fieldsSize := make(map[string][3]int, len(fields))
	for i := range fields {
		fieldsSize[fields[i]] = allFieldsSize[fields[i]]
	}
	u.displayResult(searchHeader, labels, fields, fieldsSize, data)
}

func (u *UserSelectHandler) proxyAsset(asset model.PermAsset) {
	u.selectedAsset = &asset
	permAssetDetail, err := u.h.jmsService.GetUserPermAssetDetailById(u.user.ID, asset.ID)
	if err != nil {
		logger.Errorf("Get asset accounts err: %s", err)
		return
	}
	// 过滤仅支持的连接协议
	allSupportedProtocols := srvconn.SupportedProtocols()
	filterFunc := func(p string) bool {
		name := strings.ToLower(p)
		for i := range allSupportedProtocols {
			if strings.EqualFold(name, allSupportedProtocols[i]) {
				return true
			}
		}
		return false
	}
	protocols := make([]string, 0, len(permAssetDetail.PermedProtocols))
	for i := range permAssetDetail.PermedProtocols {
		if filterFunc(permAssetDetail.PermedProtocols[i].Name) {
			protocols = append(protocols, permAssetDetail.PermedProtocols[i].Name)
		}
	}
	protocol, ok := u.h.chooseAssetProtocol(protocols)
	if !ok {
		logger.Info("Not select protocol")
		return
	}
	i18nLang := u.h.i18nLang
	lang := i18n.NewLang(i18nLang)
	if err2 := srvconn.IsSupportedProtocol(protocol); err2 != nil {
		var errMsg string
		switch {
		case errors.As(err2, &srvconn.ErrNoClient{}):
			errMsg = lang.T("%s protocol client not installed.")
			errMsg = fmt.Sprintf(errMsg, protocol)
		default:
			errMsg = lang.T("Terminal does not support protocol %s, please use web terminal to access")
			errMsg = fmt.Sprintf(errMsg, protocol)
		}
		utils.IgnoreErrWriteString(u.h.term, utils.WrapperWarn(errMsg))
		return
	}
	supportAccounts := u.filterValidAccount(permAssetDetail.PermedAccounts)
	selectedAccount, ok := u.h.chooseAccount(supportAccounts)
	if !ok {
		logger.Info("Not select account")
		return
	}
	u.selectedAccount = &selectedAccount
	req := service.SuperConnectTokenReq{
		UserId:        u.user.ID,
		AssetId:       asset.ID,
		Account:       selectedAccount.Alias,
		Protocol:      protocol,
		ConnectMethod: "ssh",
		RemoteAddr:    u.h.sess.RemoteAddr(),
	}
	tokenInfo, err := u.h.jmsService.CreateSuperConnectToken(&req)
	if err != nil {
		if tokenInfo.Code == "" {
			logger.Errorf("Create connect token and auth info failed: %s", err)
			utils.IgnoreErrWriteString(u.h.term, lang.T("Core API failed"))
			return
		}
		switch tokenInfo.Code {
		case model.ACLReject:
			logger.Errorf("Create connect token and auth info failed: %s", tokenInfo.Detail)
			utils.IgnoreErrWriteString(u.h.term, lang.T("ACL reject"))
			utils.IgnoreErrWriteString(u.h.term, utils.CharNewLine)
			return
		case model.ACLReview:
			reviewHandler := LoginReviewHandler{
				readWriter: u.h.sess,
				i18nLang:   u.h.i18nLang,
				user:       u.user,
				jmsService: u.h.jmsService,
				req:        &req,
			}
			ok2, err2 := reviewHandler.WaitReview(u.h.sess.Context())
			if err2 != nil {
				logger.Errorf("Wait login review failed: %s", err)
				utils.IgnoreErrWriteString(u.h.term, lang.T("Core API failed"))
				return
			}
			if !ok2 {
				logger.Error("Wait login review failed")
				return
			}
			tokenInfo = reviewHandler.tokenInfo
		default:
			logger.Errorf("Create connect token and auth info failed: %s %s", tokenInfo.Code, tokenInfo.Detail)
			return
		}
	}

	connectToken, err := u.h.jmsService.GetConnectTokenInfo(tokenInfo.ID)
	if err != nil {
		logger.Errorf("connect token err: %s", err)
		utils.IgnoreErrWriteString(u.h.term, lang.T("get connect token err"))
		return
	}
	proxyOpts := make([]proxy.ConnectionOption, 0, 10)
	proxyOpts = append(proxyOpts, proxy.ConnectTokenAuthInfo(&connectToken))
	proxyOpts = append(proxyOpts, proxy.ConnectI18nLang(i18nLang))
	srv, err := proxy.NewServer(u.h.sess, u.h.jmsService, proxyOpts...)
	if err != nil {
		logger.Errorf("create proxy server err: %s", err)
		return
	}
	srv.Proxy()
}

func (u *UserSelectHandler) isHiddenField(field string) bool {
	fieldName := strings.ToLower(field)
	if isBuiltinFields(fieldName) {
		return false
	}
	_, ok := u.hiddenFields[fieldName]
	return ok
}

func (u *UserSelectHandler) filterValidAccount(accounts []model.PermAccount) []model.PermAccount {
	ret := make([]model.PermAccount, 0, len(accounts))
	for i := range accounts {
		// 匿名账号不显示
		if accounts[i].IsAnonymous() {
			continue
		}
		ret = append(ret, accounts[i])
	}
	return ret
}

var builtinFields = map[string]struct{}{
	"id":      {},
	"name":    {},
	"address": {},
	"comment": {},
}

func isBuiltinFields(field string) bool {
	fieldName := strings.ToLower(field)
	_, ok := builtinFields[fieldName]
	return ok
}
