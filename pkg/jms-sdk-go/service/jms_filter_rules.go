package service

import (
	"fmt"
	"github.com/jumpserver/koko/pkg/jms-sdk-go/model"
)

func (s *JMService) GetSystemUserFilterRules(systemUserID string) (rules []model.SystemUserFilterRule, err error) {
	/*[
	    {
	        "id": "12ae03a4-81b7-43d9-b356-2db4d5d63927",
	        "org_id": "",
	        "type": "command",
			"type_display": "命令",
	        "priority": 50,
	        "content": "reboot\r\nrm",
			action_display: "拒绝",
	        action: 0,
	        "comment": "",
	        "date_created": "2019-04-29 11:32:12 +0800",
	        "date_updated": "2019-04-29 11:32:12 +0800",
	        "created_by": "Administrator",
	        "filter": "de7693ca-75d5-4639-986b-44ed390260a0"
	    },
	    {
	        "id": "c1fe1ebf-8fdc-4477-b2cf-dd9bc12de832",
	        "org_id": "",
	        "type": "regex",
	        "type_display": "正则表达式",
	        "priority": 49,
	        "content": "shutdown|echo|df",
	        "action_display": "允许"
	        "action": 1,
	        "comment": "",
	        "date_created": "2019-04-29 11:32:39 +0800",
	        "date_updated": "2019-04-29 11:32:50 +0800",
	        "created_by": "Administrator",
	        "filter": "de7693ca-75d5-4639-986b-44ed390260a0"
	    }
	]`*/
	Url := fmt.Sprintf(SystemUserCmdFilterRulesListURL, systemUserID)
	_, err = s.authClient.Get(Url, &rules)
	return
}
