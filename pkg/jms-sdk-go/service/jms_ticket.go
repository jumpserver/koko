package service

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/jumpserver/koko/pkg/jms-sdk-go/model"
)

func (s *JMService) SubmitCommandConfirm(sid string, ruleId string, cmd string) (res model.CommandTicketInfo, err error) {
	data := map[string]string{
		"session_id":         sid,
		"cmd_filter_rule_id": ruleId,
		"run_command":        cmd,
	}
	_, err = s.authClient.Post(CommandConfirmURL, data, &res)
	return
}

func (s *JMService) CheckIfNeedAssetLoginConfirm(userId, assetId, systemUserId,
	sysUsername string) (res model.AssetLoginTicketInfo, err error) {
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

func (s *JMService) CancelConfirmByRequestInfo(req model.ReqInfo) (err error) {
	res := make(map[string]interface{})
	err = s.sendRequestByRequestInfo(req, &res)
	return
}

func (s *JMService) CheckConfirmStatusByRequestInfo(req model.ReqInfo) (res model.TicketState, err error) {
	err = s.sendRequestByRequestInfo(req, &res)
	return
}

func (s *JMService) sendRequestByRequestInfo(req model.ReqInfo, res interface{}) (err error) {
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
