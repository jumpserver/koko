package model

import (
	"encoding/json"
	"testing"
)

func TestSystemUserFilterRule_Match(t *testing.T) {
	var rule SystemUserFilterRule
	ruleJson := `
    {
        "type": "command",
        "priority": 50,
        "content": "reboot\r\nrm\r\nmkdir",
        "action":0
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

	ruleJson2 := `
    {
        "type": "command",
        "priority": 50,
        "content": "reboot\nrm\nmkdir",
        "action":0
    }`

	ruleJson3 := `
    {
        "type": "command",
        "priority": 50,
        "content": "reboot\rrm\rmkdir",
        "action":0
    }`
	var cmds = []string{"rm 123", "reboot ", "mkdir"}
	for i, item := range []string{ruleJson, ruleJson2, ruleJson3} {
		err = json.Unmarshal([]byte(item), &rule)
		if err != nil {
			t.Fatalf("Unmarshal error: %s", err)
		}
		cmd := cmds[i]
		action, msg = rule.Match(cmd)
		if action != ActionDeny {
			t.Fatal("Rule should deny, but not")
		}
		t.Logf("Deny command `%s` because of `%s`", cmd, msg)
	}
}
