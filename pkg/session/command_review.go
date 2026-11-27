package session

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"unicode"

	"github.com/jumpserver-dev/sdk-go/model"
	"github.com/jumpserver-dev/sdk-go/service"
	"github.com/jumpserver/koko/pkg/i18n"
	"github.com/jumpserver/koko/pkg/logger"
)

var ErrUnknownCommandReviewState = errors.New("unknown command review state")

// PollCommandReview waits for a terminal ticket state. next owns scheduling,
// cancellation and permission checks; retry decides whether request failures or
// unknown states can be retried. A nil retry stops on either. Ticket cleanup
// remains with the caller, which may need to persist cancellation for recovery.
func PollCommandReview(next func() (model.TicketState, error), retry func(model.TicketState, error) bool) (model.TicketState, error) {
	for {
		state, err := next()
		if err == nil {
			switch state.State {
			case model.TicketOpen:
				continue
			case model.TicketApproved, model.TicketRejected, model.TicketClosed:
				return state, nil
			default:
				err = ErrUnknownCommandReviewState
			}
		}
		if retry == nil || !retry(state, err) {
			return state, err
		}
	}
}

func SubmitCommandReview(api *service.JMService, sessionID, aclID, command string, lang i18n.LanguageCode) (ticket model.CommandTicketInfo, err error) {
	client := api.Copy()
	if lang != "" {
		client.SetHeader("Accept-Language", lang.CoreCode())
	}
	var body json.RawMessage
	response, err := client.Call(http.MethodPost, service.AclCommandReviewURL, map[string]string{
		"session_id": sessionID, "cmd_filter_acl_id": aclID, "run_command": command,
	}, &body)
	if err == nil && json.Unmarshal(body, &ticket) == nil && ticket.CheckReq.URL != "" {
		return ticket, nil
	}
	logger.Errorf("Session %s: command review submission failed", sessionID)
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

// OrderedCommandACLs returns a session-owned copy in SDK decision order.
func OrderedCommandACLs(source []model.CommandACL) model.CommandACLs {
	result := make(model.CommandACLs, len(source))
	for index := range source {
		result[index] = source[index]
		result[index].CommandGroups = append(
			[]model.CommandFilterItem(nil), source[index].CommandGroups...,
		)
	}
	sort.Stable(result)
	return result
}
