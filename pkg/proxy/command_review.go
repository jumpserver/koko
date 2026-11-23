package proxy

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"unicode"

	"github.com/jumpserver-dev/sdk-go/model"
	"github.com/jumpserver-dev/sdk-go/service"
	"github.com/jumpserver/koko/pkg/i18n"
	"github.com/jumpserver/koko/pkg/logger"
)

func submitCommandReview(api *service.JMService, sessionID, aclID, command string, lang i18n.LanguageCode) (ticket model.CommandTicketInfo, err error) {
	client := api.Copy()
	client.SetHeader("Accept-Language", lang.CoreCode())
	var body json.RawMessage
	response, err := client.Call(http.MethodPost, service.AclCommandReviewURL, map[string]string{
		"session_id": sessionID, "cmd_filter_acl_id": aclID, "run_command": command,
	}, &body)
	if err == nil {
		err = json.Unmarshal(body, &ticket)
		if err == nil && ticket.CheckReq.URL != "" {
			return ticket, nil
		}
		if err == nil {
			err = errors.New("missing command review status URL")
		}
	}
	logger.Errorf("Session %s: submit command confirm api err: %s", sessionID, err)
	// Only expose structured client errors, never transport errors or server traces.
	if response != nil && response.StatusCode >= 400 && response.StatusCode < 500 {
		var messages []string
		if json.Unmarshal(body, &messages) != nil {
			var detail struct {
				Detail string `json:"detail"`
			}
			if json.Unmarshal(body, &detail) == nil {
				messages = []string{detail.Detail}
			}
		}
		reason := strings.TrimSpace(strings.Map(func(r rune) rune {
			if unicode.IsControl(r) {
				return ' '
			}
			return r
		}, strings.Join(messages, " ")))
		if reason != "" {
			return ticket, fmt.Errorf(lang.T("Command review submission failed. The command was not executed. Reason: %s"), reason)
		}
	}
	return ticket, errors.New(lang.T("Command review submission failed. The command was not executed. Please contact your administrator."))
}
