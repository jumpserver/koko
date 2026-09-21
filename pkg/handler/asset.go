package handler

import (
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/mattn/go-runewidth"
	"golang.org/x/term"

	"github.com/jumpserver-dev/sdk-go/model"
	"github.com/jumpserver-dev/sdk-go/service"

	"github.com/jumpserver/koko/pkg/i18n"
	"github.com/jumpserver/koko/pkg/logger"
	"github.com/jumpserver/koko/pkg/proxy"
	"github.com/jumpserver/koko/pkg/srvconn"
	"github.com/jumpserver/koko/pkg/sshcert"
	"github.com/jumpserver/koko/pkg/utils"
)

func (u *UserSelectHandler) retrieveRemoteAsset(reqParam model.PaginationParam) []model.PermAsset {
	res, err := u.h.jmsService.GetUserPermsAssets(u.user.ID, reqParam)
	if err != nil {
		logger.Errorf("Get user perm assets failed: %s", err.Error())
	}
	assets := u.updateRemotePageData(reqParam, res)
	return u.prepareAssetPage(assets, u.assetListPath())
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

func (u *UserSelectHandler) searchLocalTypeAsset(searches ...string) []model.PermAsset {
	assets := u.searchLocalAsset(searches...)
	filtered := make([]model.PermAsset, 0, len(assets))
	for _, asset := range assets {
		if u.selectedType.Category != "" && string(asset.Category) != u.selectedType.Category {
			continue
		}
		if u.selectedType.AssetType != "" && string(asset.Type) != u.selectedType.AssetType {
			continue
		}
		filtered = append(filtered, asset)
	}
	return filtered
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

const maxFieldSize = 80

func (u *UserSelectHandler) displayAssets(searchHeader string) {
	currentResult := u.currentResult
	lang := i18n.NewLang(u.h.i18nLang)
	idLabel := lang.T("Number")
	nameLabel := lang.T("Name")
	addressLabel := lang.T("Address")
	platformLabel := lang.T("Platform")
	orgLabel := lang.T("Organization")
	commentLabel := lang.T("Comment")
	idFieldSize := runewidth.StringWidth(idLabel)
	nameFieldSize := len(nameLabel)
	addressFieldSize := len(addressLabel)
	platformFieldSize := len(platformLabel)
	organizationFieldSize := len(orgLabel)
	commentFieldSize := len(commentLabel)
	data := make([]map[string]string, len(currentResult))
	firstNumber, _ := resultDisplayRange(u.CurrentOffSet(), len(currentResult), u.TotalCount())
	for i := range currentResult {
		item := &u.currentResult[i]
		row := make(map[string]string)
		idNumber := strconv.Itoa(firstNumber + i)
		row["ID"] = idNumber
		name := strings.ReplaceAll(item.Name, " ", "_")
		if !u.assetCanConnect(i) {
			name = "⊘ " + name
		}
		row["Name"] = name
		row["Address"] = item.Address
		row["Platform"] = item.Platform.Name
		row["Organization"] = item.OrgName
		row["Comment"] = joinMultiLineString(item.Comment)
		data[i] = row
		if idFieldSize < len(idNumber) {
			idFieldSize = len(idNumber)
		}
		if len(name) > nameFieldSize {
			nameFieldSize = len(name)
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

func (u *UserSelectHandler) showUnavailableAsset(asset model.PermAsset) {
	message := u.h.tr("当前资产不支持 Terminal 连接", "This asset does not support Terminal connections")
	if !asset.IsActive {
		message = u.h.tr("当前资产已被禁用，无法连接", "This asset is disabled and cannot be connected")
	} else {
		detail, err := u.h.assetClient(asset.OrgID).GetUserPermAssetDetailById(u.user.ID, asset.ID)
		if err != nil {
			logger.Errorf("Get unavailable asset detail failed: %s", err)
		} else {
			current := make([]string, 0, len(detail.PermedProtocols))
			for _, protocol := range detail.PermedProtocols {
				if name := strings.TrimSpace(protocol.Name); name != "" {
					current = append(current, name)
				}
			}
			currentText := u.h.tr("无可用协议", "No available protocols")
			if len(current) > 0 {
				currentText = strings.Join(current, ", ")
			}
			message = fmt.Sprintf(u.h.tr(
				"当前资产的协议（%s）不支持，无法连接。Terminal 支持：%s",
				"This asset's protocols (%s) are unsupported. Terminal supports: %s",
			), currentText, strings.Join(srvconn.SupportedProtocols(), ", "))
		}
	}
	utils.IgnoreErrWriteString(u.h.term, utils.WrapperWarn(message))
}

func GetInputUsername(sess io.ReadWriteCloser) (username string, err error) {
	vt := term.NewTerminal(sess, "username: ")
	count := 0
	for count < 3 {
		username, err = vt.ReadLine()
		if err != nil {
			return "", err
		}
		username = strings.TrimSpace(username)
		if username != "" {
			return username, nil
		}
		count++
	}
	return "", errors.New("input username exceed max retry")
}

// connectSelectedAsset runs token authorization, approval and auditing inside
// the selected terminal.
func connectSelectedAsset(conn proxy.UserConnection, jmsService *service.JMService,
	user *model.User, asset model.PermAsset, selectedAccount model.PermAccount, protocol, i18nLang string,
	passwordInputGuard func() error) (connected bool) {
	lang := i18n.NewLang(i18nLang)
	if conn.Context().Err() != nil {
		return
	}
	req := service.SuperConnectTokenReq{
		UserId:        user.ID,
		AssetId:       asset.ID,
		Account:       selectedAccount.Alias,
		Protocol:      protocol,
		ConnectMethod: "ssh",
		InputUsername: selectedAccount.Username,
		RemoteAddr:    conn.RemoteAddr(),
	}
	if selectedAccount.IsInputUser() {
		inputUsername, err1 := GetInputUsername(conn)
		if err1 != nil {
			logger.Errorf("Get input username err: %s", err1)
			return
		}
		req.InputUsername = inputUsername
	}

	tokenInfo, err := jmsService.CreateSuperConnectToken(&req)
	if err != nil {
		if tokenInfo.Code == "" {
			logger.Errorf("Create connect token and auth info failed: %s", err)
			utils.IgnoreErrWriteString(conn, lang.T("Core API failed"))
			return
		}
		switch tokenInfo.Code {
		case model.ACLReject:
			logger.Errorf("Create connect token and auth info failed: %s", tokenInfo.Detail)
			utils.IgnoreErrWriteString(conn, utils.WrapperWarn(lang.T("ACL reject")))
			utils.IgnoreErrWriteString(conn, utils.CharNewLine)
			return
		case model.ACLFaceVerify, model.ACLFaceOnline, model.ACLFaceOnlineNotSupported:
			// todo: 需要人脸验证 后续需要发站内信通知用户，并且等待用户人脸验证通过
			logger.Errorf("Create connect token and auth info failed: %s %s", tokenInfo.Code, tokenInfo.Detail)
			msg := lang.T("Face ACL is not supported yet. Please use the WebTerminal to connect the asset.")
			utils.IgnoreErrWriteString(conn, utils.WrapperWarn(msg))
			utils.IgnoreErrWriteString(conn, utils.CharNewLine)
			return
		case model.ACLReview:
			reviewHandler := LoginReviewHandler{
				readWriter: conn,
				i18nLang:   i18nLang,
				user:       user,
				jmsService: jmsService,
				req:        &req,
			}
			ok2, err2 := reviewHandler.WaitReview(conn.Context())
			if err2 != nil {
				logger.Errorf("Wait login review failed: %s", err)
				utils.IgnoreErrWriteString(conn, lang.T("Core API failed"))
				return
			}
			if !ok2 {
				logger.Error("Wait login review failed")
				return
			}
			tokenInfo = reviewHandler.tokenInfo
		default:
			msg := lang.T("Unknown error code: %s, detail: %s")
			utils.IgnoreErrWriteString(conn, fmt.Sprintf(msg, tokenInfo.Code, tokenInfo.Detail))
			utils.IgnoreErrWriteString(conn, utils.CharNewLine)
			logger.Errorf("Create connect token and auth info failed: %s %s", tokenInfo.Code, tokenInfo.Detail)
			return
		}
	}

	if conn.Context().Err() != nil {
		return
	}
	connectToken, err := sshcert.GetConnectTokenInfo(jmsService, tokenInfo.ID, true)
	if err != nil {
		logger.Errorf("connect token err: %s", err)
		utils.IgnoreErrWriteString(conn, lang.T("get connect token err"))
		return
	}
	defer connectToken.ClearSSHCertificateCredential()
	proxyOpts := make([]proxy.ConnectionOption, 0, 10)
	proxyOpts = append(proxyOpts, proxy.ConnectTokenAuthInfo(&connectToken))
	proxyOpts = append(proxyOpts, proxy.ConnectI18nLang(i18nLang))
	proxyOpts = append(proxyOpts, proxy.ConnectPasswordInputGuard(passwordInputGuard))
	if conn.Context().Err() != nil {
		return
	}
	srv, err := proxy.NewServer(conn, jmsService, proxyOpts...)
	if err != nil {
		logger.Errorf("create proxy server err: %s", err)
		return
	}
	srv.Proxy()
	return srv.SessionEndReason != model.ReasonErrConnectFailed
}

func (u *UserSelectHandler) proxyAsset(asset model.PermAsset) {
	u.selectedAsset = &asset
	client := u.h.assetClient(asset.OrgID)
	permAssetDetail, err := client.GetUserPermAssetDetailById(u.user.ID, asset.ID)
	if err != nil {
		logger.Errorf("Get asset accounts err: %s", err)
		lang := i18n.NewLang(u.h.i18nLang)
		utils.IgnoreErrWriteString(u.h.term, utils.WrapperWarn(lang.T("Core API failed")))
		utils.IgnoreErrWriteString(u.h.term, utils.CharNewLine)
		return
	}
	if permAssetDetail.ID != asset.ID || permAssetDetail.OrgID != "" && asset.OrgID != "" &&
		asset.OrgID != tuiGlobalOrganizationID && permAssetDetail.OrgID != asset.OrgID {
		logger.Errorf("Classic text mode asset detail does not match selected asset %s", asset.ID)
		utils.IgnoreErrWriteString(u.h.term, u.h.tr("资产信息不匹配", "Asset details do not match the selected asset"))
		utils.IgnoreErrWriteString(u.h.term, utils.CharNewLine)
		return
	}
	if permAssetDetail.OrgID != "" {
		if permAssetDetail.OrgID != asset.OrgID {
			client = u.h.assetClient(permAssetDetail.OrgID)
		}
		asset.OrgID = permAssetDetail.OrgID
	}

	allSupportedProtocols := srvconn.SupportedProtocols()
	filterFunc := func(protocol string) bool {
		for i := range allSupportedProtocols {
			if strings.EqualFold(protocol, allSupportedProtocols[i]) {
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
	if err = srvconn.IsSupportedProtocol(protocol); err != nil {
		var errMsg string
		switch {
		case errors.As(err, &srvconn.ErrNoClient{}):
			errMsg = fmt.Sprintf(lang.T("%s protocol client not installed."), protocol)
		default:
			errMsg = fmt.Sprintf(lang.T("Terminal does not support protocol %s, please use web terminal to access"), protocol)
		}
		utils.IgnoreErrWriteString(u.h.term, utils.WrapperWarn(errMsg))
		return
	}
	supportAccounts := u.filterValidAccount(permAssetDetail.PermedAccounts)
	selectedAccount, ok, back := u.h.chooseAccount(supportAccounts)
	if !ok {
		logger.Info("Not select account")
		if back {
			u.DisplayCurrentResult()
		}
		return
	}
	u.selectedAccount = &selectedAccount
	if u.h.preferences != nil {
		u.h.preferences.storeConnection(u.user.ID, tuiAssetPreferenceKey(asset), tuiConnectionPreference{
			Account: tuiAccountPreferenceKey(selectedAccount), Protocol: protocol,
		})
	}
	passwordKey := tuiPasswordAttemptKey(asset, selectedAccount, protocol)
	passwordLimitError := fmt.Errorf(u.h.tr(
		"手动密码最多允许输入 %d 次",
		"Manual password can be entered at most %d times",
	), maxTUIManualPasswordAttempts)
	connectSelectedAsset(u.h.sess, client, u.user, asset, selectedAccount, protocol, i18nLang, func() error {
		if u.h.manualPasswords.acquire(passwordKey) {
			return nil
		}
		return passwordLimitError
	})
}

func (h *InteractiveHandler) assetClient(orgID string) *service.JMService {
	client := newLangAPIClient(h.jmsService, h.i18nLang)
	if orgID != "" && orgID != tuiGlobalOrganizationID {
		client.SetHeader("X-JMS-ORG", orgID)
	}
	client.SetHeader("Connection", "close")
	return client
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
		if accounts[i].IsAnonymous() {
			continue
		}
		ret = append(ret, accounts[i])
	}
	return ret
}
