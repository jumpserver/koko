package model

import (
	"encoding/json"
	"testing"
)

func TestSystemUserFilterRule_Match(t *testing.T) {
	var rule SystemUserFilterRule
	ruleJson := `
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
    }`
	err := json.Unmarshal([]byte(ruleJson), &rule)
	if err != nil {
		t.Error("Unmarshal error: ", err)
	}

	action, msg := rule.Match("reboot 123")
	if action != ActionDeny {
		t.Error("Rule should deny reboot, but not")
	}
	if msg != "reboot" {
		t.Error("Msg is not reboot")
	}
}
