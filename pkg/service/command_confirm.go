package service

func SubmitCommandConfirm(sid string, ruleId string, cmd string) (res ConfirmResponse, err error) {
	/*
		{
		session_id: sid,
		rule_id : ruleId,
		command: cmd,
		}
	*/
	data := map[string]string{
		"session_id":         sid,
		"cmd_filter_rule_id": ruleId,
		"run_command":        cmd,
	}
	_, err = authClient.Post(CommandConfirmURL, data, &res)
	return
}

type ConfirmResponse struct {
	CheckConfirmStatus requestInfo `json:"check_confirm_status"`
	CloseConfirm       requestInfo `json:"close_confirm"`
	TicketDetailUrl    string      `json:"ticket_detail_url"`
	Reviewers          []string    `json:"reviewers"`
}
