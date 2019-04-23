package service

import (
	"cocogo/pkg/logger"
	"cocogo/pkg/model"
	"fmt"
)

//
//func GetSystemUserAssetAuthInfo(systemUserID, assetID string) (authInfo model.SystemUserAuthInfo, err error) {
//
//
//	err = json.Unmarshal(buf, &authInfo)
//	if err != nil {
//		log.Info(err)
//		return authInfo, err
//	}
//	return authInfo, err
//
//}
//
func GetSystemUserAuthInfo(systemUserID string) {
	var authInfo model.SystemUserAuthInfo
	systemUrl := fmt.Sprintf(urls["SystemUserAuthInfo"], systemUserID)
	err := client.Get(systemUrl, &authInfo, true)
	if err != nil {
		logger.Info("get User Assets Groups err:", err)
		return
	}
}
