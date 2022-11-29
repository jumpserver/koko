package auth

import (
	"context"
	"time"

	"github.com/jumpserver/koko/pkg/jms-sdk-go/model"
	"github.com/jumpserver/koko/pkg/jms-sdk-go/service"
	"github.com/jumpserver/koko/pkg/logger"
)

type connectionConfirmOption struct {
	user    *model.User
	account *model.Account

	assetId string
}

func NewLoginConfirm(jmsService *service.JMService, opts ...ConfirmOption) LoginConfirmService {
	var option connectionConfirmOption
	for _, setter := range opts {
		setter(&option)
	}
	return LoginConfirmService{option: &option, jmsService: jmsService}
}

type LoginConfirmService struct {
	jmsService *service.JMService

	option *connectionConfirmOption

	checkReqInfo    model.ReqInfo
	cancelReqInfo   model.ReqInfo
	reviewers       []string
	ticketDetailUrl string

	processor string // 此审批的处理人
	ticketId  string // 此工单 Id
}

func (c *LoginConfirmService) CheckIsNeedLoginConfirm() (bool, error) {
	/*
		1. 连接登录是否需要审批
	*/
	userID := c.option.user.ID
	assetId := c.option.assetId
	username := c.option.account.Username
	res, err := c.jmsService.CheckIfNeedAssetLoginConfirm(userID, assetId, username)
	if err != nil {
		return false, err
	}
	c.ticketId = res.TicketId
	c.reviewers = res.Reviewers
	c.checkReqInfo = res.CheckReq
	c.cancelReqInfo = res.CloseReq
	c.ticketDetailUrl = res.TicketDetailUrl
	return res.NeedConfirm, nil

}

func (c *LoginConfirmService) WaitLoginConfirm(ctx context.Context) Status {
	return c.waitConfirmFinish(ctx)
}

func (c *LoginConfirmService) GetReviewers() []string {
	reviewers := make([]string, len(c.reviewers))
	copy(reviewers, c.reviewers)
	return reviewers
}

func (c *LoginConfirmService) GetTicketUrl() string {
	return c.ticketDetailUrl
}

func (c *LoginConfirmService) GetProcessor() string {
	return c.processor
}

func (c *LoginConfirmService) GetTicketId() string {
	return c.ticketId
}

func (c *LoginConfirmService) waitConfirmFinish(ctx context.Context) Status {
	// 10s 请求一次
	t := time.NewTicker(10 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			c.cancelConfirm()
			return StatusCancel
		case <-t.C:
			statusRes, err := c.jmsService.CheckConfirmStatusByRequestInfo(c.checkReqInfo)
			if err != nil {
				logger.Errorf("Check confirm status err: %s", err.Error())
				continue
			}
			switch statusRes.State {
			case model.TicketOpen:
				continue
			case model.TicketApproved:
				c.processor = statusRes.Processor
				return StatusApprove
			case model.TicketRejected, model.TicketClosed:
				c.processor = statusRes.Processor
				return StatusReject
			default:
				logger.Errorf("Receive unknown login confirm status %s",
					statusRes.Status)
			}
		}
	}
}

func (c *LoginConfirmService) cancelConfirm() {
	if err := c.jmsService.CancelConfirmByRequestInfo(c.cancelReqInfo); err != nil {
		logger.Errorf("Cancel confirm request err: %s", err.Error())
	}
}

type Status int

const (
	StatusApprove Status = iota + 1
	StatusReject
	StatusCancel
)

type ConfirmOption func(*connectionConfirmOption)

func ConfirmWithUser(user *model.User) ConfirmOption {
	return func(option *connectionConfirmOption) {
		option.user = user
	}
}

func ConfirmWithAccount(account *model.Account) ConfirmOption {
	return func(option *connectionConfirmOption) {
		option.account = account
	}
}

func ConfirmWithAssetId(Id string) ConfirmOption {
	return func(option *connectionConfirmOption) {
		option.assetId = Id
	}
}
