package service

import (
	"fmt"

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
