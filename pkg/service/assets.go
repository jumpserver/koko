package service

import (
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
func GetSystemUserAssetAuthInfo(systemUserID, assetID string) (info model.SystemUserAuthInfo, err error) {
	var authInfo model.SystemUserAuthInfo
	err = Client.Get("systemUserAuthInfo", nil, &authInfo)
	if err != nil {
		logger.Info("get User Assets Groups err:", err)
		return
	}
	return
}
