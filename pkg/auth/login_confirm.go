package auth

import (
	"context"
	"time"

	"github.com/jumpserver-dev/sdk-go/model"
	"github.com/jumpserver-dev/sdk-go/service"
	"github.com/jumpserver/koko/pkg/logger"
)

type reviewOption struct {
	user *model.User
	Info *model.ConnectTokenInfo
}

func NewLoginReview(jmsService *service.JMService, opts ...ReviewOption) LoginReviewService {
	var option reviewOption
	for _, setter := range opts {
		setter(&option)
	}
	ticketInfo := option.Info.TicketInfo
	checkReqInfo := ticketInfo.CheckReq
	cancelReqInfo := ticketInfo.CloseReq
	ticketDetail := ticketInfo.TicketDetailUrl
	reviewers := ticketInfo.Reviewers
	return LoginReviewService{jmsService: jmsService, option: &option,
		checkReqInfo: checkReqInfo, cancelReqInfo: cancelReqInfo,
		ticketDetailUrl: ticketDetail, reviewers: reviewers}
}

type LoginReviewService struct {
	jmsService *service.JMService

	option *reviewOption

	checkReqInfo    model.ReqInfo
	cancelReqInfo   model.ReqInfo
	reviewers       []string
	ticketDetailUrl string

	processor string // 此审批的处理人
}

func (c *LoginReviewService) WaitLoginConfirm(ctx context.Context) Status {
	return c.waitConfirmFinish(ctx)
}

func (c *LoginReviewService) GetReviewers() []string {
	reviewers := make([]string, len(c.reviewers))
	copy(reviewers, c.reviewers)
	return reviewers
}

func (c *LoginReviewService) GetTicketUrl() string {
	return c.ticketDetailUrl
}

func (c *LoginReviewService) GetProcessor() string {
	return c.processor
}

func (c *LoginReviewService) waitConfirmFinish(ctx context.Context) Status {
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
					statusRes.State)
			}
		}
	}
}

func (c *LoginReviewService) cancelConfirm() {
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

type ReviewOption func(*reviewOption)

func WithReviewUser(user *model.User) ReviewOption {
	return func(option *reviewOption) {
		option.user = user
	}
}

func WithReviewTokenInfo(info *model.ConnectTokenInfo) ReviewOption {
	return func(option *reviewOption) {
		option.Info = info
	}
}
