package service

import (
	"fmt"
	"net/http"
	"strings"
)

func (s *JMService) SubmitCommandConfirm(sid string, ruleId string, cmd string) (res ConfirmResponse, err error) {
	data := map[string]string{
		"session_id":         sid,
		"cmd_filter_rule_id": ruleId,
		"run_command":        cmd,
	}
	_, err = s.authClient.Post(CommandConfirmURL, data, &res)
	return
}

func (s *JMService) CheckIfNeedAssetLoginConfirm(userId, assetId, systemUserId,
	sysUsername string) (res CheckAssetConfirmResponse, err error) {
	data := map[string]string{
		"user_id":              userId,
		"asset_id":             assetId,
		"system_user_id":       systemUserId,
		"system_user_username": sysUsername,
	}

	_, err = s.authClient.Post(AssetLoginConfirmURL, data, &res)
	return
}

func (s *JMService) CheckIfNeedAppConnectionConfirm(userID, assetID, systemUserID string) (bool, error) {

	return false, nil
}

func (s *JMService) CancelConfirmByRequestInfo(req RequestInfo) (err error) {
	res := make(map[string]interface{})
	err = s.sendRequestByRequestInfo(req, &res)
	return
}

func (s *JMService) CheckConfirmStatusByRequestInfo(req RequestInfo) (res ConfirmStatusResponse, err error) {
	err = s.sendRequestByRequestInfo(req, &res)
	return
}

func (s *JMService) sendRequestByRequestInfo(req RequestInfo, res interface{}) (err error) {
	switch strings.ToUpper(req.Method) {
	case http.MethodGet:
		_, err = s.authClient.Get(req.URL, res)
	case http.MethodDelete:
		_, err = s.authClient.Delete(req.URL, res)
	default:
		err = fmt.Errorf("unsupport method %s", req.Method)
	}
	return
}

type ConfirmResponse struct {
	CheckConfirmStatus RequestInfo `json:"check_confirm_status"`
	CloseConfirm       RequestInfo `json:"close_confirm"`
	TicketDetailUrl    string      `json:"ticket_detail_url"`
	Reviewers          []string    `json:"reviewers"`
}

type RequestInfo struct {
	Method string `json:"method"`
	URL    string `json:"url"`
}

type ConfirmStatusResponse struct {
	Status    string `json:"status"`
	Action    string `json:"action"`
	Processor string `json:"processor"`
}

type CheckAssetConfirmResponse struct {
	NeedConfirm        bool        `json:"need_confirm"`
	CheckConfirmStatus RequestInfo `json:"check_confirm_status"`
	CloseConfirm       RequestInfo `json:"close_confirm"`
	TicketDetailUrl    string      `json:"ticket_detail_url"`
	Reviewers          []string    `json:"reviewers"`
}
