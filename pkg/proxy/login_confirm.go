package proxy

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jumpserver/koko/pkg/i18n"
	"github.com/jumpserver/koko/pkg/logger"
	"github.com/jumpserver/koko/pkg/service"
	"github.com/jumpserver/koko/pkg/utils"
)

// 校验用户登录资产是否需要复核
func validateLoginConfirm(srv *service.LoginConfirmService, userCon UserConnection) bool {
	ok, err := srv.CheckIsNeedLoginConfirm()
	if err != nil {
		logger.Errorf("Conn[%s] validate login confirm api err: %s",
			userCon.ID(), err.Error())
		msg := i18n.T("validate Login confirm err: Core Api failed")
		utils.IgnoreErrWriteString(userCon, msg)
		utils.IgnoreErrWriteString(userCon, utils.CharNewLine)
		return false
	}
	if !ok {
		logger.Debugf("Conn[%s] no need login confirm", userCon.ID())
		return true
	}

	ctx, cancelFunc := context.WithCancel(userCon.Context())
	term := utils.NewTerminal(userCon, "")
	defer userCon.Close()
	go func() {
		defer cancelFunc()
		for {
			line, err := term.ReadLine()
			if err != nil {
				logger.Errorf("Wait confirm user readLine exit: %s", err.Error())
				return
			}
			switch line {
			case "quit", "q":
				logger.Infof("Conn[%s] quit confirm", userCon.ID())
				return
			}
		}
	}()
	reviewers := srv.GetReviewers()
	detailURL := srv.GetTicketUrl()
	titleMsg := i18n.T("Need ticket confirm to login, already send email to the reviewers")
	reviewersMsg := fmt.Sprintf(i18n.T("Ticket Reviewers: %s"), strings.Join(reviewers, ", "))
	detailURLMsg := fmt.Sprintf(i18n.T("Could copy website URL to notify reviewers: %s"), detailURL)
	waitMsg := i18n.T("Please waiting for the reviewers to confirm, enter q to exit. ")
	utils.IgnoreErrWriteString(userCon, titleMsg)
	utils.IgnoreErrWriteString(userCon, utils.CharNewLine)
	utils.IgnoreErrWriteString(userCon, reviewersMsg)
	utils.IgnoreErrWriteString(userCon, utils.CharNewLine)
	utils.IgnoreErrWriteString(userCon, detailURLMsg)
	utils.IgnoreErrWriteString(userCon, utils.CharNewLine)
	go func() {
		delay := 0
		for {
			select {
			case <-ctx.Done():
				return
			default:
				delayS := fmt.Sprintf("%ds", delay)
				data := strings.Repeat("\x08", len(delayS)+len(waitMsg)) + waitMsg + delayS
				utils.IgnoreErrWriteString(userCon, data)
				time.Sleep(time.Second)
				delay += 1
			}
		}
	}()

	status := srv.WaitLoginConfirm(ctx)
	cancelFunc()
	processor := srv.GetProcessor()
	var success bool
	statusMsg := i18n.T("Unknown status")
	switch status {
	case service.StatusApprove:
		// 审核通过
		formatMsg := i18n.T("%s approved")
		statusMsg = utils.WrapperString(fmt.Sprintf(formatMsg, processor), utils.Green)
		success = true
	case service.StatusReject:
		// 审核未通过
		formatMsg := i18n.T("%s rejected")
		statusMsg = utils.WrapperString(fmt.Sprintf(formatMsg, processor), utils.Red)
	case service.StatusCancel:
		// 审核取消
		statusMsg = utils.WrapperString(i18n.T("Cancel confirm"), utils.Red)
	}
	logger.Infof("Conn[%s] Login Confirm result: %s", userCon.ID(), statusMsg)
	utils.IgnoreErrWriteString(userCon, utils.CharNewLine)
	utils.IgnoreErrWriteString(userCon, statusMsg)
	utils.IgnoreErrWriteString(userCon, utils.CharNewLine)
	return success
}
