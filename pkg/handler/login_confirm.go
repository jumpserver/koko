package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/gliderlabs/ssh"
	"github.com/jumpserver-dev/sdk-go/model"
	"github.com/jumpserver-dev/sdk-go/service"
	"github.com/jumpserver/koko/pkg/i18n"
	"golang.org/x/term"
)

type connectionACLState struct {
	Active     bool             `json:"is_active"`
	ExpireTime int              `json:"expire_time"`
	Ticket     *model.ObjectId  `json:"from_ticket"`
	TicketInfo model.TicketInfo `json:"from_ticket_info"`
	ACL        struct {
		Requirements map[string]string `json:"requirements"`
		Error        string            `json:"error"`
		Cancelled    bool              `json:"cancelled"`
		FaceOnline   bool              `json:"face_online"`
		ReviewStatus string            `json:"review_status"`
		Reviewers    []struct {
			Name     string `json:"name"`
			Username string `json:"username"`
		} `json:"reviewers"`
		Preparation struct {
			Status      string `json:"status"`
			Detail      string `json:"detail"`
			ExecutionID string `json:"execution_id"`
		} `json:"preparation"`
		MFAMethods []struct {
			Value             string `json:"value"`
			Label             string `json:"label"`
			ChallengeRequired bool   `json:"challenge_required"`
		} `json:"mfa_methods"`
	} `json:"acl"`
}

var connectionACLSteps = []struct{ key, title string }{
	{"reason", "Connection reason"}, {"mfa", "MFA verification"},
	{"face", "Face verification"}, {"review", "Connection approval"}, {"push_account", "Account push"},
}

func connectionACLErrorMessage(locale string, err error) string {
	lang := i18n.NewLang(locale)
	if errors.Is(err, context.Canceled) || errors.Is(err, io.EOF) {
		return lang.T("Connection cancelled.")
	}
	return userFacingErrorMessage(lang.T("Connection failed"), err)
}

// Display Core metadata as text, never as terminal control sequences.
func connectionACLText(value string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, value)
}

func connectionACLResponseError(data json.RawMessage, status int, lang i18n.LanguageCode) error {
	var fields map[string]json.RawMessage
	_ = json.Unmarshal(data, &fields)
	messages, seen := make([]string, 0), make(map[string]bool)
	for _, field := range []string{"detail", "reason", "mfa_code", "mfa_type", "step", "non_field_errors"} {
		var message string
		var values []string
		if json.Unmarshal(fields[field], &message) == nil {
			values = []string{message}
		} else if json.Unmarshal(fields[field], &values) != nil {
			values = nil
		}
		for _, value := range values {
			value = strings.TrimSpace(connectionACLText(lang.T(value)))
			if value != "" && !seen[value] {
				seen[value] = true
				messages = append(messages, value)
			}
		}
	}
	if len(messages) > 0 {
		return errors.New(strings.Join(messages, "; "))
	}
	if status >= http.StatusBadRequest {
		return fmt.Errorf(lang.T("Request failed (HTTP %d)."), status)
	}
	return errors.New(lang.T("Invalid response from server."))
}

// Classic text and TUI panes complete explicit steps on the same token.
func completeConnectionACL(ctx context.Context, core *service.JMService, tokenID string, rw io.ReadWriteCloser, locale string) error {
	lang := i18n.NewLang(locale)
	newTerminal := func() *term.Terminal {
		terminal := term.NewTerminal(rw, "")
		if session, ok := rw.(interface{ Pty() ssh.Pty }); ok {
			win := session.Pty().Window
			if win.Width > 0 && win.Height > 0 {
				_ = terminal.SetSize(win.Width, win.Height)
			}
		}
		return terminal
	}
	terminal := newTerminal()
	path := fmt.Sprintf("/api/v1/authentication/super-connection-token/%s/acl/", tokenID)
	call := func(method string, body map[string]interface{}) (connectionACLState, int, error) {
		var state connectionACLState
		var data json.RawMessage
		response, err := core.Call(method, path, body, &data)
		status := 0
		if response != nil {
			status = response.StatusCode
			if err != nil {
				err = connectionACLResponseError(data, status, lang)
			} else if json.Unmarshal(data, &state) != nil {
				err = connectionACLResponseError(nil, status, lang)
			}
		}
		if ctx.Err() != nil {
			err = ctx.Err()
		}
		return state, status, err
	}
	printLine := func(message string) { _, _ = fmt.Fprint(terminal, message+"\r\n") }
	readLine := func(prompt string) (string, error) {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		terminal.SetPrompt(lang.T(prompt))
		line, err := terminal.ReadLine()
		// Status output must not redraw the previous step's input prompt.
		terminal.SetPrompt("")
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		if strings.EqualFold(strings.TrimSpace(line), "q") {
			return "", context.Canceled
		}
		return strings.TrimSpace(line), err
	}
	confirm := func(prompt string) error {
		for {
			answer, err := readLine(prompt)
			if err != nil {
				return err
			}
			switch strings.ToLower(answer) {
			case "y", "yes":
				return nil
			case "", "n", "no":
				return context.Canceled
			default:
				printLine(lang.T("Enter y to continue or n to cancel."))
			}
		}
	}
	completed := false
	defer func() {
		terminal.SetPrompt("")
		if !completed {
			_, _, _ = call(http.MethodPost, map[string]interface{}{"cancel": true})
		}
	}()
	// A read cannot create an approval request or start account provisioning.
	state, _, err := call(http.MethodGet, nil)
	if err != nil {
		return err
	}
	stepCount := 0
	for _, step := range connectionACLSteps {
		if _, exists := state.ACL.Requirements[step.key]; exists {
			stepCount++
		}
	}
	if stepCount > 0 {
		printLine(lang.T("Enter q and press Enter to cancel at any step."))
	}
	currentStep, mfaMethod := "", ""
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := state.connectionError(lang); err != nil {
			if state.ACL.Preparation.ExecutionID != "" {
				printLine(fmt.Sprintf(lang.T("Task ID: %s"), connectionACLText(state.ACL.Preparation.ExecutionID)))
			}
			return err
		}
		// Native SSH has no trusted camera path; fail closed before collecting inputs.
		faceStatus, needsFace := state.ACL.Requirements["face"]
		if state.ACL.FaceOnline || (needsFace && faceStatus != "passed") {
			return errors.New(lang.T("Face ACL is not supported yet. Please use the WebTerminal to connect the asset."))
		}
		step, title, index := "", "", 0
		for _, candidate := range connectionACLSteps {
			if status, exists := state.ACL.Requirements[candidate.key]; exists {
				index++
				if status != "passed" {
					step, title = candidate.key, lang.T(candidate.title)
					break
				}
			}
		}
		if state.Active && step == "" {
			completed = true
			return nil
		}
		if state.ExpireTime <= 0 {
			return errors.New(lang.T("Connection token expired"))
		}
		if step == "" {
			return errors.New(lang.T("Connection is not ready. Start a new connection."))
		}
		if currentStep != step {
			if stepCount > 1 {
				title = fmt.Sprintf("[%d/%d] %s", index, stepCount, title)
			}
			printLine(title)
			currentStep = step
		}
		body := map[string]interface{}{"step": step}
		switch step {
		case "reason":
			reason, readErr := readLine("Connection reason: ")
			if readErr != nil {
				return readErr
			}
			if reason == "" || utf8.RuneCountInString(reason) > 4096 {
				printLine(lang.T("Enter a connection reason (up to 4096 characters)."))
				continue
			}
			body["reason"] = reason
		case "mfa":
			methods := state.ACL.MFAMethods
			if len(methods) == 0 {
				return errors.New(lang.T("Enroll MFA in the web console before connecting"))
			}
			if mfaMethod == "" {
				selected := 0
				if len(methods) > 1 {
					for i, method := range methods {
						printLine(fmt.Sprintf("%d. %s", i+1, connectionACLText(method.Label)))
					}
					choice, readErr := readLine("MFA method [1]: ")
					if readErr != nil {
						return readErr
					}
					if choice != "" {
						selected, err = strconv.Atoi(choice)
						if err != nil || selected < 1 || selected > len(methods) {
							printLine(lang.T("Select a listed MFA method."))
							continue
						}
						selected--
					}
				}
				mfaMethod = methods[selected].Value
				if methods[selected].ChallengeRequired {
					if _, _, err := call(http.MethodPost, map[string]interface{}{"step": step, "mfa_send": true, "mfa_type": mfaMethod}); err != nil {
						return err
					}
					printLine(lang.T("MFA code sent."))
				}
			}
			code, readErr := readLine("MFA code: ")
			if readErr != nil {
				return readErr
			}
			if code == "" {
				printLine(lang.T("Enter the MFA code."))
				continue
			}
			body["mfa_type"], body["mfa_code"] = mfaMethod, code
		case "review":
			if state.Ticket == nil && state.TicketInfo.TicketDetailUrl == "" {
				if err := confirm("Approval is required. Submit a request? (y/n): "); err != nil {
					return err
				}
				printLine(lang.T("Submitting approval request..."))
			} else {
				state, err = waitConnectionACL(ctx, state, step, terminal, rw, lang, call)
				if err != nil {
					return err
				}
				// Discard unfinished input from the waiting prompt before the next step.
				terminal = newTerminal()
				printLine(lang.T("Approval granted."))
				continue
			}
		case "push_account":
			if state.ACL.Preparation.Status == "" {
				if err := confirm("Push the same-name account before connecting? (y/n): "); err != nil {
					return err
				}
				printLine(lang.T("Starting account push..."))
			} else {
				state, err = waitConnectionACL(ctx, state, step, terminal, rw, lang, call)
				if err != nil {
					return err
				}
				terminal = newTerminal()
				printLine(lang.T("Account is ready."))
				continue
			}
		}
		next, status, requestErr := call(http.MethodPost, body)
		if requestErr != nil {
			if status == http.StatusBadRequest && (step == "reason" || step == "mfa") && ctx.Err() == nil {
				printLine(connectionACLText(userFacingErrorDetail(requestErr)))
				continue
			}
			return requestErr
		}
		state = next
		if state.ACL.Requirements[step] == "passed" {
			messages := map[string]string{"reason": "Connection reason saved.", "mfa": "MFA verification passed.", "review": "Approval granted.", "push_account": "Account is ready."}
			printLine(lang.T(messages[step]))
		}
	}
}

func (s connectionACLState) connectionError(lang i18n.LanguageCode) error {
	if s.ACL.Cancelled {
		return context.Canceled
	}
	if s.ACL.ReviewStatus == "rejected" {
		return errors.New(lang.T("Approval rejected. Connection cancelled."))
	}
	if s.ACL.ReviewStatus == "closed" {
		return errors.New(lang.T("Approval request closed. Connection cancelled."))
	}
	if s.ACL.Preparation.Status == "failed" || s.ACL.Requirements["push_account"] == "failed" {
		return errors.New(lang.T("Account push failed. Contact an administrator."))
	}
	if s.ACL.Error != "" {
		return errors.New(connectionACLText(lang.T(s.ACL.Error)))
	}
	return nil
}

func waitConnectionACL(ctx context.Context, state connectionACLState, step string, terminal *term.Terminal, rw io.ReadWriteCloser,
	lang i18n.LanguageCode, call func(string, map[string]interface{}) (connectionACLState, int, error)) (result connectionACLState, resultErr error) {
	printLine := func(message string) { _, _ = fmt.Fprint(terminal, message+"\r\n") }
	if step == "review" {
		reviewers := make([]string, 0, len(state.ACL.Reviewers))
		for _, user := range state.ACL.Reviewers {
			name := user.Name
			if name == "" {
				name = user.Username
			} else if user.Username != "" && user.Username != name {
				name += " (" + user.Username + ")"
			}
			reviewers = append(reviewers, name)
		}
		if len(reviewers) == 0 {
			reviewers = state.TicketInfo.Reviewers
		}
		printLine(fmt.Sprintf(lang.T("Ticket Reviewers: %s"), connectionACLText(strings.Join(reviewers, ", "))))
		printLine(fmt.Sprintf(lang.T("Approval ticket: %s"), connectionACLText(state.TicketInfo.TicketDetailUrl)))
		printLine(lang.T("Waiting for approval."))
	}
	terminal.SetPrompt(lang.T("Cancel connection (q + Enter): "))
	done, stopping, input := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	go func() {
		defer close(done)
		for {
			line, err := terminal.ReadLine()
			if err != nil {
				select {
				case <-stopping:
					if !errors.Is(err, io.ErrClosedPipe) {
						input <- err
					}
				default:
					input <- err
				}
				return
			}
			if strings.EqualFold(strings.TrimSpace(line), "q") {
				input <- context.Canceled
				return
			}
		}
	}()
	defer func() {
		close(stopping)
		// Interrupt the waiting reader without closing the TUI connection.
		select {
		case <-done:
		default:
			if reader, ok := rw.(interface{ InterruptRead() error }); ok {
				_ = reader.InterruptRead()
			} else {
				_ = rw.Close()
			}
		}
		<-done
		terminal.SetPrompt("")
		// The old terminal may retain an unfinished line. Clear it without
		// redrawing that input, and leave status messages on their own line.
		_, _ = rw.Write([]byte("\r\x1b[2K\r\n"))
		select {
		case err := <-input:
			resultErr = err
		default:
		}
		if ctx.Err() != nil {
			resultErr = ctx.Err()
		}
	}()
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	lastProgress := ""
	for {
		if err := state.connectionError(lang); err != nil {
			return state, err
		}
		if state.ACL.Requirements[step] == "passed" {
			return state, nil
		}
		if state.ExpireTime <= 0 {
			return state, errors.New(lang.T("Connection token expired"))
		}
		if step == "push_account" && state.ACL.Preparation.Status != lastProgress {
			lastProgress = state.ACL.Preparation.Status
			message := "Account push is running."
			if lastProgress == "queued" {
				message = "Account push is queued."
			}
			printLine(lang.T(message))
			if state.ACL.Preparation.ExecutionID != "" {
				printLine(fmt.Sprintf(lang.T("Task ID: %s"), connectionACLText(state.ACL.Preparation.ExecutionID)))
			}
		}
		select {
		case <-ctx.Done():
			return state, ctx.Err()
		case err := <-input:
			return state, err
		case <-ticker.C:
		}
		next, _, err := call(http.MethodPost, map[string]interface{}{"step": step})
		if err != nil {
			return state, err
		}
		state = next
	}
}
