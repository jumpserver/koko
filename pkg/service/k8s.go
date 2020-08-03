package service

import (
	"fmt"
	"strconv"

	"github.com/jumpserver/koko/pkg/logger"
	"github.com/jumpserver/koko/pkg/model"
)

func ValidateUserK8sPermission(userID, clusterID, systemUserID string) bool {
	payload := map[string]string{
		"user_id":        userID,
		"k8s_app_id":     clusterID,
		"system_user_id": systemUserID,
	}
	Url := ValidateUserK8sPermissionURL
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

func GetUserK8sClusters(userID string, pageSize, offset int, searches ...string) (resp model.K8sClustersPaginationResponse) {
	if pageSize < 0 {
		pageSize = 0
	}
	paramsArray := make([]map[string]string, 0, len(searches)+2)
	for i := 0; i < len(searches); i++ {
		paramsArray = append(paramsArray, map[string]string{
			"search": searches[i],
		})
	}
	params := map[string]string{
		"limit":  strconv.Itoa(pageSize),
		"offset": strconv.Itoa(offset),
	}
	paramsArray = append(paramsArray, params)
	Url := fmt.Sprintf(K8sPemClustersURL, userID)
	var err error
	if pageSize > 0 {
		_, err = authClient.Get(Url, &resp, paramsArray...)
	} else {
		var data []model.K8sCluster
		_, err = authClient.Get(Url, &data, paramsArray...)
		resp.Data = data
		resp.Total = len(data)
	}
	if err != nil {
		logger.Error("Get user K8s Clusters error: ", err)
	}
	return
}

func GetUserK8sSystemUsers(userID, k8sId string) (sysUsers []model.SystemUser) {
	Url := fmt.Sprintf(K8sSystemUsersURL, userID, k8sId)
	_, err := authClient.Get(Url, &sysUsers)
	if err != nil {
		logger.Error("Get user k8s system users error: ", err)
	}
	return
}

func GetUserK8sAuthToken(systemUserID string) (info model.SystemUserAuthInfo) {
	Url := fmt.Sprintf(SystemUserAuthURL, systemUserID)
	_, err := authClient.Get(Url, &info)
	if err != nil {
		logger.Errorf("Get system user %s auth info failed", systemUserID)
	}
	return
}

func GetK8sCluster(k8sId string) (res model.K8sCluster) {
	Url := fmt.Sprintf(K8sClusterDetailURL, k8sId)
	_, err := authClient.Get(Url, &res)
	if err != nil {
		logger.Errorf("Get User k8s err: %s", err)
	}
	return
}
