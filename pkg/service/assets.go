package service

import (
	"fmt"

	"github.com/jumpserver/koko/pkg/logger"
	"github.com/jumpserver/koko/pkg/model"
)

func GetSystemUserAssetAuthInfo(systemUserID, assetID string) (info model.SystemUserAuthInfo) {
	return getUserAssetAuthInfo(systemUserID, assetID, "", "")
}

func GetUserAssetAuthInfo(systemUserID, assetID, userID, username string) (info model.SystemUserAuthInfo) {
	return getUserAssetAuthInfo(systemUserID, assetID, userID, username)
}

func getUserAssetAuthInfo(systemUserID, assetID, userID, username string) (info model.SystemUserAuthInfo) {
	Url := fmt.Sprintf(SystemUserAssetAuthURL, systemUserID, assetID)
	params := make(map[string]string)
	if username != "" {
		params["username"] = username
	}
	if userID != "" {
		params["user_id"] = userID
	}
	_, err := authClient.Get(Url, &info, params)
	if err != nil {
		logger.Errorf("Get system user %s asset %s auth info failed：%s", systemUserID, assetID, err)
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
	Url := fmt.Sprintf(SystemUserCmdFilterRulesListURL, systemUserID)

	_, err = authClient.Get(Url, &rules)
	if err != nil {
		logger.Errorf("Get system user %s filter rule failed", systemUserID)
	}
	return
}

func GetSystemUser(systemUserID string) (info model.SystemUser) {
	Url := fmt.Sprintf(SystemUserDetailURL, systemUserID)
	_, err := authClient.Get(Url, &info)
	if err != nil {
		logger.Errorf("Get system user %s failed", systemUserID)
	}
	return
}

func GetAsset(assetID string) (asset model.Asset) {
	Url := fmt.Sprintf(AssetDetailURL, assetID)
	_, err := authClient.Get(Url, &asset)
	if err != nil {
		logger.Errorf("Get Asset %s failed: %s", assetID, err)
	}
	return
}

func GetTokenAsset(token string) (tokenUser model.TokenUser) {
	Url := fmt.Sprintf(TokenAssetURL, token)
	_, err := authClient.Get(Url, &tokenUser)
	if err != nil {
		logger.Error("Get Token Asset info failed: ", err)
	}
	return
}

func GetAssetGateways(assetID string) (gateways []model.Gateway) {
	Url := fmt.Sprintf(AssetGatewaysURL, assetID)
	_, err := authClient.Get(Url, &gateways)
	if err != nil {
		logger.Errorf("Get Asset %s gateways failed: %s", assetID, err)
	}
	return
}
