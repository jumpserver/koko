package auth

import (
	"context"
	"time"

	"github.com/jumpserver/koko/pkg/jms-sdk-go/model"
	"github.com/jumpserver/koko/pkg/jms-sdk-go/service"
	"github.com/jumpserver/koko/pkg/logger"
)

type connectionConfirmOption struct {
	user       *model.User
	systemUser *model.SystemUserAuthInfo

	targetType string
	targetID   string
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

	checkReqInfo    service.RequestInfo
	cancelReqInfo   service.RequestInfo
	reviewers       []string
	ticketDetailUrl string

	processor string // 此审批的处理人
	ticketId  string // 此工单 Id
}

func (c *LoginConfirmService) CheckIsNeedLoginConfirm() (bool, error) {
	userID := c.option.user.ID
	systemUserID := c.option.systemUser.ID
	systemUsername := c.option.systemUser.Username
	targetID := c.option.targetID
	switch c.option.targetType {
	case model.AppType:
		return c.jmsService.CheckIfNeedAppConnectionConfirm(userID, targetID, systemUserID)
	default:
		res, err := c.jmsService.CheckIfNeedAssetLoginConfirm(userID, targetID,
			systemUserID, systemUsername)
		if err != nil {
			return false, err
		}
		c.ticketId = res.TicketId
		c.reviewers = res.Reviewers
		c.checkReqInfo = res.CheckConfirmStatus
		c.cancelReqInfo = res.CloseConfirm
		c.ticketDetailUrl = res.TicketDetailUrl
		return res.NeedConfirm, nil
	}
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
			switch statusRes.Status {
			case approve:
				c.processor = statusRes.Processor
				return StatusApprove
			case reject:
				c.processor = statusRes.Processor
				return StatusReject
			case await:
				continue
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

const (
	approve = "approved"
	reject  = "rejected"
	await   = "await"
)

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

func ConfirmWithSystemUser(sysUser *model.SystemUserAuthInfo) ConfirmOption {
	return func(option *connectionConfirmOption) {
		option.systemUser = sysUser
	}
}

func ConfirmWithTargetType(targetType string) ConfirmOption {
	return func(option *connectionConfirmOption) {
		option.targetType = targetType
	}
}

func ConfirmWithTargetID(targetID string) ConfirmOption {
	return func(option *connectionConfirmOption) {
		option.targetID = targetID
	}
}
