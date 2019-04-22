package service

import (
	"fmt"

	"cocogo/pkg/logger"
	"cocogo/pkg/model"
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
	err := client.Get("systemUserAuthInfo", nil, &authInfo)
	if err != nil {
		logger.Info("get User Assets Groups err:", err)
		return
	}
	fmt.Println(authInfo)
}
