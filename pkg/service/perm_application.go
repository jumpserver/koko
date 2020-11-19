package service

import (
	"fmt"
	"github.com/jumpserver/koko/pkg/logger"
	"github.com/jumpserver/koko/pkg/model"
)

func GetAllUserPermMySQLs(userId string) []map[string]interface{} {
	var param model.PaginationParam
	res := GetUserPermsMySQL(userId, param)
	return res.Data
}

func GetAllUserPermK8s(userId string) []map[string]interface{} {
	var param model.PaginationParam
	res := GetUserPermsK8s(userId, param)
	return res.Data
}

func GetUserPermsMySQL(userId string, param model.PaginationParam) model.PaginationResponse {
	reqUrl := fmt.Sprintf(UserPermsApplicationsURL, userId, model.AppTypeMySQL)
	return getPaginationResult(reqUrl, param)
}

func GetUserPermsK8s(userId string, param model.PaginationParam) model.PaginationResponse {
	reqUrl := fmt.Sprintf(UserPermsApplicationsURL, userId, model.AppTypeK8s)
	return getPaginationResult(reqUrl, param)
}

func getApplicationDetail(appId string, res interface{}) {
	reqUrl := fmt.Sprintf(ApplicationDetailURL, appId)
	_, err := authClient.Get(reqUrl, res)
	if err != nil {
		logger.Errorf("Get Application err: %s", err)
	}
	return
}
func GetMySQLApplication(appId string) (res model.DatabaseApplication) {
	getApplicationDetail(appId, &res)
	return
}

func GetK8sApplication(appId string) (res model.K8sApplication) {
	getApplicationDetail(appId, &res)
	return
}

func GetUserApplicationSystemUsers(userId, appId string) (res []model.SystemUser) {
	reqUrl := fmt.Sprintf(UserPermsApplicationSystemUsersURL, userId, appId)
	_, err := authClient.Get(reqUrl, &res)
	if err != nil {
		logger.Errorf("Get Application system user err: %s", err)
	}
	return
}

func ValidateUserApplicationPermission(userId, appId, systemUserId string) bool {
	payload := map[string]string{
		"user_id":        userId,
		"application_id": appId,
		"system_user_id": systemUserId,
	}
	Url := ValidateApplicationPermissionURL
	var res struct {
		Msg bool `json:"msg"`
	}
	_, err := authClient.Get(Url, &res, payload)

	if err != nil {
		logger.Error(err)
		return false
	}

	return res.Msg
}

func GetApplicationSystemUserAuthInfo(systemUserId string) (info model.SystemUserAuthInfo) {
	Url := fmt.Sprintf(SystemUserAuthURL, systemUserId)
	_, err := authClient.Get(Url, &info)
	if err != nil {
		logger.Errorf("Get system user %s auth info failed", systemUserId)
	}
	return
}
