package service

import (
	"cocogo/pkg/logger"
	"cocogo/pkg/model"
	"encoding/json"
	"fmt"
)

func GetSystemUserAssetAuthInfo(systemUserID, assetID string) (info model.SystemUserAuthInfo) {
	return
}

func GetSystemUserAuthInfo(systemUserID string) (info model.SystemUserAuthInfo) {
	Url := fmt.Sprintf(SystemUserAuthInfoURL, systemUserID)

	err := client.Get(Url, &info, true)
	if err != nil {
		logger.Error("Get system user auth info failed")
	}
	return
}

func GetSystemUserFilterRules(systemUsrId string) (rules []model.SystemUserFilterRule, err error) {
	var resp = `[
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
]`
	err = json.Unmarshal([]byte(resp), &rules)
	return
}
