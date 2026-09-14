package handler

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"golang.org/x/term"

	"github.com/jumpserver-dev/sdk-go/model"
	"github.com/jumpserver-dev/sdk-go/service"

	"github.com/jumpserver/koko/pkg/i18n"
	"github.com/jumpserver/koko/pkg/logger"
	"github.com/jumpserver/koko/pkg/proxy"
	"github.com/jumpserver/koko/pkg/sshcert"
	"github.com/jumpserver/koko/pkg/utils"
)

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
// the selected TUI terminal.
func connectSelectedAsset(conn proxy.UserConnection, jmsService *service.JMService,
	user *model.User, asset model.PermAsset, selectedAccount model.PermAccount, protocol, i18nLang string) {
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
	if conn.Context().Err() != nil {
		return
	}
	srv, err := proxy.NewServer(conn, jmsService, proxyOpts...)
	if err != nil {
		logger.Errorf("create proxy server err: %s", err)
		return
	}
	srv.Proxy()
}
