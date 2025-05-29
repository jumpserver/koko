package handler

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"golang.org/x/term"

	"github.com/jumpserver-dev/sdk-go/model"
	"github.com/jumpserver-dev/sdk-go/service"
	"github.com/jumpserver/koko/pkg/auth"
	"github.com/jumpserver/koko/pkg/i18n"
	"github.com/jumpserver/koko/pkg/logger"
	"github.com/jumpserver/koko/pkg/utils"
)

// 校验用户登录资产是否需要复核

type LoginReviewHandler struct {
	i18nLang   string
	readWriter io.ReadWriteCloser
	jmsService *service.JMService
	user       *model.User
	req        *service.SuperConnectTokenReq

	tokenInfo model.ConnectTokenInfo
}

func (l *LoginReviewHandler) GetTokenInfo() model.ConnectTokenInfo {
	return l.tokenInfo
}

func (l *LoginReviewHandler) WaitReview(ctx context.Context) (bool, error) {
	lang := i18n.NewLang(l.i18nLang)
	vt := term.NewTerminal(l.readWriter, lang.T("Need ACL review, continue? (y/n): "))
	utils.IgnoreErrWriteString(vt, utils.CharNewLine)
	count := 0
	for {
		okMsg, err2 := vt.ReadLine()
		if err2 != nil {
			logger.Errorf("Wait confirm user readLine exit: %s", err2.Error())
			return false, err2
		}
		count++
		if okMsg == "" && count < 3 {
			continue
		}
		if count >= 3 || strings.ToLower(okMsg) != "y" {
			logger.Info("ACL review required cancel")
			utils.IgnoreErrWriteString(vt, lang.T("Cancel to login asset or max 3 retry"))
			utils.IgnoreErrWriteString(vt, utils.CharNewLine)
			return false, nil
		}
		if strings.ToLower(okMsg) == "y" {
			break
		}

	}

	l.req.Params = map[string]string{"create_ticket": "true"}
	tokenInfo, err := l.jmsService.CreateSuperConnectToken(l.req)
	if err != nil {
		logger.Errorf("Create connect token and auth info failed: %s", err)
		utils.IgnoreErrWriteString(vt, lang.T("Core API failed"))
		return false, err
	}
	l.tokenInfo = tokenInfo
	srv := auth.NewLoginReview(l.jmsService,
		auth.WithReviewUser(l.user),
		auth.WithReviewTokenInfo(&tokenInfo))
	return l.WaitTicketReview(ctx, &srv)
}

func (l *LoginReviewHandler) WaitTicketReview(ctx context.Context, srv *auth.LoginReviewService) (bool, error) {
	lang := i18n.NewLang(l.i18nLang)
	ctx, cancelFunc := context.WithCancel(ctx)
	vt := term.NewTerminal(l.readWriter, " ")
	go func() {
		defer cancelFunc()
		for {
			line, err := vt.ReadLine()
			if err != nil {
				logger.Errorf("Wait confirm user readLine exit: %s", err.Error())
				return
			}
			switch line {
			case "quit", "q":
				logger.Infof("User %s quit confirm", l.user.String())
				return
			}
		}
	}()
	reviewers := srv.GetReviewers()
	detailURL := srv.GetTicketUrl()
	titleMsg := lang.T("Need ticket confirm to login, already send email to the reviewers")
	reviewersMsg := fmt.Sprintf(lang.T("Ticket Reviewers: %s"), strings.Join(reviewers, ", "))
	detailURLMsg := fmt.Sprintf(lang.T("Could copy website URL to notify reviewers: %s"), detailURL)
	waitMsg := lang.T("Please waiting for the reviewers to confirm, enter q to exit. ")
	utils.IgnoreErrWriteString(vt, titleMsg)
	utils.IgnoreErrWriteString(vt, utils.CharNewLine)
	utils.IgnoreErrWriteString(vt, reviewersMsg)
	utils.IgnoreErrWriteString(vt, utils.CharNewLine)
	utils.IgnoreErrWriteString(vt, detailURLMsg)
	utils.IgnoreErrWriteString(vt, utils.CharNewLine)
	go func() {
		delay := 0
		for {
			select {
			case <-ctx.Done():

				return
			default:
				delayS := fmt.Sprintf("%ds", delay)
				data := strings.Repeat("\x08", len(delayS)+len(waitMsg)) + waitMsg + delayS
				utils.IgnoreErrWriteString(vt, data)
				time.Sleep(time.Second)
				delay += 1
			}
		}
	}()

	status := srv.WaitLoginConfirm(ctx)
	cancelFunc()
	l.readWriter.Close()
	processor := srv.GetProcessor()
	var success bool
	statusMsg := lang.T("Unknown status")
	switch status {
	case auth.StatusApprove:
		// 审核通过
		formatMsg := lang.T("%s approved")
		statusMsg = utils.WrapperString(fmt.Sprintf(formatMsg, processor), utils.Green)
		success = true
	case auth.StatusReject:
		// 审核未通过
		formatMsg := lang.T("%s rejected")
		statusMsg = utils.WrapperString(fmt.Sprintf(formatMsg, processor), utils.Red)
	case auth.StatusCancel:
		// 审核取消
		statusMsg = utils.WrapperString(lang.T("Cancel confirm"), utils.Red)
	}
	logger.Infof("User %s Login Confirm result: %s", l.user.String(), statusMsg)
	utils.IgnoreErrWriteString(vt, utils.CharNewLine)
	utils.IgnoreErrWriteString(vt, statusMsg)
	utils.IgnoreErrWriteString(vt, utils.CharNewLine)
	return success, nil
}
