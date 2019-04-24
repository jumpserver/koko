package service

import (
	"cocogo/pkg/logger"
	"cocogo/pkg/sdk"
	"fmt"
)

func GetSystemUserAssetAuthInfo(systemUserID, assetID string) (info sdk.SystemUserAuthInfo) {
	return
}

func GetSystemUserAuthInfo(systemUserID string) (info sdk.SystemUserAuthInfo) {
	Url := fmt.Sprintf(sdk.SystemUserAuthInfoURL, systemUserID)

	err := client.Get(Url, &info, true)
	if err != nil {
		logger.Error("Get system user auth info failed")
	}
	return
}
