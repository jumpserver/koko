package service

import (
	"fmt"

	"cocogo/pkg/logger"
	"cocogo/pkg/model"
)

func GetSystemUserAssetAuthInfo(systemUserID, assetID string) (info model.SystemUserAuthInfo) {
	Url := authClient.ParseUrlQuery(fmt.Sprintf(SystemUserAssetAuthURL, systemUserID, assetID), nil)
	err := authClient.Get(Url, &info)
	if err != nil {
		logger.Error("Get system user Asset auth info failed")
	}
	return
}

func GetSystemUserAuthInfo(systemUserID string) (info model.SystemUserAuthInfo) {
	Url := authClient.ParseUrlQuery(fmt.Sprintf(SystemUserAuthInfoURL, systemUserID), nil)

	err := authClient.Get(Url, &info)
	if err != nil {
		logger.Error("Get system user auth info failed")
	}
	return
}

func GetSystemUserFilterRules(systemUserID string) (rules []model.SystemUserFilterRule, err error) {
	/*[
	    {
	        "id": "12ae03a4-81b7-43d9-b356-2db4d5d63927",
	        "org_id": "",
	        "type": {
	            "value": "command",
	            "display": "命令"
	        },
	        "priority": 50,
	        "content": "reboot\r\nrm",
	        "action": {
	            "value": 0,
	            "display": "拒绝"
	        },
	        "comment": "",
	        "date_created": "2019-04-29 11:32:12 +0800",
	        "date_updated": "2019-04-29 11:32:12 +0800",
	        "created_by": "Administrator",
	        "filter": "de7693ca-75d5-4639-986b-44ed390260a0"
	    },
	    {
	        "id": "c1fe1ebf-8fdc-4477-b2cf-dd9bc12de832",
	        "org_id": "",
	        "type": {
	            "value": "regex",
	            "display": "正则表达式"
	        },
	        "priority": 49,
	        "content": "shutdown|echo|df",
	        "action": {
	            "value": 1,
	            "display": "允许"
	        },
	        "comment": "",
	        "date_created": "2019-04-29 11:32:39 +0800",
	        "date_updated": "2019-04-29 11:32:50 +0800",
	        "created_by": "Administrator",
	        "filter": "de7693ca-75d5-4639-986b-44ed390260a0"
	    }
	]`*/
	Url := authClient.ParseUrlQuery(fmt.Sprintf(SystemUserCmdFilterRules, systemUserID), nil)

	err = authClient.Get(Url, &rules)
	if err != nil {
		logger.Error("Get system user auth info failed")
	}
	return
}

func GetSystemUser(systemUserID string) (info model.SystemUser) {
	Url := authClient.ParseUrlQuery(fmt.Sprintf(SystemUser, systemUserID), nil)
	err := authClient.Get(Url, &info)
	if err != nil {
		logger.Errorf("Get system user %s failed", systemUserID)
	}
	return
}

func GetAsset(assetID string) (asset model.Asset) {
	Url := authClient.ParseUrlQuery(fmt.Sprintf(Asset, assetID), nil)
	err := authClient.Get(Url, &asset)
	if err != nil {
		logger.Errorf("Get Asset %s failed", assetID)
	}
	return
}

func GetTokenAsset(token string) (tokenUser model.TokenUser) {
	Url := authClient.ParseUrlQuery(fmt.Sprintf(TokenAsset, token), nil)
	err := authClient.Get(Url, &tokenUser)
	if err != nil {
		logger.Error("Get Token Asset info failed")
	}
	return
}
