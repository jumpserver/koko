package service

import (
	"fmt"
	"strconv"

	"github.com/jumpserver/koko/pkg/logger"
	"github.com/jumpserver/koko/pkg/model"
)

func GetUserDatabases(uid string) (res []model.Database) {
	Url := fmt.Sprintf(DatabaseAPPURL, uid)
	_, err := authClient.Get(Url, &res)
	if err != nil {
		logger.Errorf("Get User databases err: %s", err)
	}
	return
}

func GetUserDatabaseSystemUsers(userID, assetID string) (sysUsers []model.SystemUser) {
	Url := fmt.Sprintf(UserDatabaseSystemUsersURL, userID, assetID)
	_, err := authClient.Get(Url, &sysUsers)
	if err != nil {
		logger.Error("Get user asset system users error: ", err)
	}
	return
}

func GetSystemUserDatabaseAuthInfo(systemUserID string) (info model.SystemUserAuthInfo) {
	Url := fmt.Sprintf(SystemUserAuthURL, systemUserID)
	_, err := authClient.Get(Url, &info)
	if err != nil {
		logger.Errorf("Get system user %s auth info failed", systemUserID)
	}
	return
}

func GetDatabase(dbID string) (res model.Database) {
	Url := fmt.Sprintf(DatabaseDetailURL, dbID)
	_, err := authClient.Get(Url, &res)
	if err != nil {
		logger.Errorf("Get User databases err: %s", err)
	}
	return
}

func GetUserPaginationDatabases(userID string, pageSize, offset int, searches ...string) (resp model.DatabasesPaginationResponse) {
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
	Url := fmt.Sprintf(DatabaseAPPURL, userID)
	var err error
	if pageSize > 0 {
		_, err = authClient.Get(Url, &resp, paramsArray...)
	} else {
		var data []model.Database
		_, err = authClient.Get(Url, &data, paramsArray...)
		resp.Data = data
		resp.Total = len(data)
	}

	if err != nil {
		logger.Errorf("Get User databases err: %s", err)
	}
	return
}
