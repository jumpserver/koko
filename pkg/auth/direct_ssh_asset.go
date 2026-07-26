package auth

import (
	"context"
	"errors"
	"strings"
	"sync"

	"github.com/gliderlabs/ssh"
	gossh "golang.org/x/crypto/ssh"

	"github.com/jumpserver-dev/sdk-go/model"
	"github.com/jumpserver/koko/pkg/config"
	"github.com/jumpserver/koko/pkg/logger"
	"github.com/jumpserver/koko/pkg/srvconn"
)

type directSSHAssetAuthState struct {
	token        *model.ConnectToken
	loginAccount model.BaseAccount
	ctxDone      <-chan struct{}
	sessionID    string

	mu            sync.Mutex
	currentPrompt *directSSHAssetPrompt
	events        chan directSSHAssetAuthEvent
}

type directSSHAssetAuthEvent struct {
	prompt   *directSSHAssetPrompt
	prepared *srvconn.PreparedSSHClient
	err      error
}

type directSSHAssetPrompt struct {
	user        string
	instruction string
	questions   []string
	echos       []bool

	answerCh chan directSSHAssetPromptAnswer
}

type directSSHAssetPromptAnswer struct {
	answers []string
	err     error
}

func prepareDirectSSHAssetAuth(ctx ssh.Context, req *DirectLoginAssetReq) error {
	if req == nil || !req.IsToken() {
		return nil
	}
	token := req.ConnectToken
	if token.Protocol != model.ProtocolSSH || !token.Asset.IsSupportProtocol(model.ProtocolSSH) {
		return nil
	}
	if !token.Actions.EnableConnect() {
		return nil
	}
	if prepared := GetPreparedDirectSSHClient(ctx, token); prepared != nil {
		return nil
	}

	state := newDirectSSHAssetAuthState(ctx.SessionID(), token, ctx.Done())
	state.startHandshake()
	event := state.waitForNextEvent()
	switch {
	case event.prepared != nil:
		storePreparedDirectSSHClient(ctx, event.prepared)
		return nil
	case event.prompt != nil:
		logger.Infof("SSH conn[%s] asset %s authentication requires keyboard-interactive input",
			ctx.SessionID(), token.Asset.String())
		ctx.SetValue(ContextKeyDirectSSHAssetAuthState, state)
		return &ssh.PartialSuccessError{Next: ssh.ServerAuthCallbacks{
			KeyboardInteractiveCallback: SSHKeyboardInteractiveAuth,
		}}
	default:
		logger.Errorf("SSH conn[%s] prepare asset %s SSH authentication failed: %v",
			ctx.SessionID(), token.Asset.String(), event.err)
		return authErr
	}
}

func continueDirectSSHAssetAuth(ctx ssh.Context,
	challenger gossh.KeyboardInteractiveChallenge) error {
	state, ok := ctx.Value(ContextKeyDirectSSHAssetAuthState).(*directSSHAssetAuthState)
	if !ok || state == nil || state.token == nil {
		logger.Errorf("SSH conn[%s] asset keyboard-interactive authentication state not found",
			ctx.SessionID())
		return authErr
	}

	for {
		prompt := state.currentPendingPrompt()
		if prompt == nil {
			event := state.waitForNextEvent()
			switch {
			case event.prompt != nil:
				prompt = event.prompt
			case event.prepared != nil:
				storePreparedDirectSSHClient(ctx, event.prepared)
				ctx.SetValue(ContextKeyDirectSSHAssetAuthState, nil)
				return nil
			default:
				ctx.SetValue(ContextKeyDirectSSHAssetAuthState, nil)
				logger.Errorf("SSH conn[%s] asset keyboard-interactive authentication failed: %v",
					ctx.SessionID(), event.err)
				return authErr
			}
		}

		answers, err := challenger(prompt.user, prompt.instruction, prompt.questions, prompt.echos)
		if submitErr := state.submitPromptAnswers(answers, err); submitErr != nil {
			ctx.SetValue(ContextKeyDirectSSHAssetAuthState, nil)
			logger.Errorf("SSH conn[%s] submit asset keyboard-interactive answers failed: %s",
				ctx.SessionID(), submitErr)
			return authErr
		}
		if err != nil {
			ctx.SetValue(ContextKeyDirectSSHAssetAuthState, nil)
			logger.Errorf("SSH conn[%s] read asset keyboard-interactive answers failed: %s",
				ctx.SessionID(), err)
			return authErr
		}
	}
}

func newDirectSSHAssetAuthState(sessionID string, token *model.ConnectToken,
	ctxDone <-chan struct{}) *directSSHAssetAuthState {
	loginAccount := token.Account.GetBaseAccount()
	if token.Account.SuFrom != nil {
		loginAccount = token.Account.SuFrom
	}
	return &directSSHAssetAuthState{
		token:        token,
		loginAccount: *loginAccount,
		ctxDone:      ctxDone,
		sessionID:    sessionID,
		events:       make(chan directSSHAssetAuthEvent, 1),
	}
}

func (s *directSSHAssetAuthState) startHandshake() {
	go func() {
		keyboardAuth := srvconn.SSHClientKeyboardAuth(
			buildDirectSSHChallengeBridge(s.sessionID, s.loginAccount,
				s.awaitClientAnswers),
		)
		extraOpts := make([]srvconn.SSHClientOption, 0, 2)
		if srvconn.IsSSHMFATarget(&s.token.Platform) && !s.loginAccount.IsSSHKey() {
			// MFA platforms must not fall back to a standalone password method,
			// because that can bypass the keyboard-interactive MFA exchange.
			extraOpts = append(extraOpts, srvconn.SSHClientPassword(""))
		}
		extraOpts = append(extraOpts, keyboardAuth)
		client, err := srvconn.NewSSHClientWithTokenAccount(
			s.token, &s.loginAccount, config.GetConf().SSHTimeout, extraOpts...,
		)
		var prepared *srvconn.PreparedSSHClient
		if err == nil {
			prepared = srvconn.NewPreparedSSHClient(s.token, client)
			go func() {
				<-s.ctxDone
				prepared.CloseIfUnused()
			}()
		}
		select {
		case s.events <- directSSHAssetAuthEvent{prepared: prepared, err: err}:
		case <-s.ctxDone:
			if prepared != nil {
				prepared.CloseIfUnused()
			}
		}
	}()
}

func buildDirectSSHChallengeBridge(sessionID string, account model.BaseAccount,
	challenger gossh.KeyboardInteractiveChallenge) gossh.KeyboardInteractiveChallenge {
	password := ""
	if !account.IsSSHKey() {
		password = account.Secret
	}
	return func(user, instruction string, questions []string,
		echos []bool) ([]string, error) {
		challengeUser := user
		if challengeUser == "" {
			challengeUser = account.Username
		}
		if len(questions) == 0 {
			if instruction == "" {
				return []string{}, nil
			}
			answers, err := challenger(challengeUser, instruction, nil, nil)
			if err != nil {
				return nil, err
			}
			if len(answers) != 0 {
				return nil, errors.New("asset keyboard-interactive informational response mismatch")
			}
			return []string{}, nil
		}

		answers := make([]string, len(questions))
		pendingQuestions := make([]string, 0, len(questions))
		pendingEchos := make([]bool, 0, len(questions))
		pendingIndexes := make([]int, 0, len(questions))
		for i := range questions {
			if password != "" && isSSHPasswordQuestion(questions[i]) {
				answers[i] = password
				continue
			}
			echo := false
			if i < len(echos) {
				echo = echos[i]
			}
			pendingQuestions = append(pendingQuestions, questions[i])
			pendingEchos = append(pendingEchos, echo)
			pendingIndexes = append(pendingIndexes, i)
		}
		if len(pendingIndexes) == 0 {
			return answers, nil
		}

		logger.Infof("SSH conn[%s] forwarding %d asset keyboard-interactive question(s) to user",
			sessionID, len(pendingIndexes))
		pendingAnswers, err := challenger(
			challengeUser, instruction, pendingQuestions, pendingEchos,
		)
		if err != nil {
			return nil, err
		}
		if len(pendingAnswers) != len(pendingIndexes) {
			return nil, errors.New("asset keyboard-interactive answers mismatch")
		}
		for i := range pendingAnswers {
			answers[pendingIndexes[i]] = pendingAnswers[i]
		}
		return answers, nil
	}
}

func isSSHPasswordQuestion(question string) bool {
	return strings.Contains(strings.ToLower(strings.TrimSpace(question)), "password")
}

func (s *directSSHAssetAuthState) awaitClientAnswers(user, instruction string,
	questions []string, echos []bool) ([]string, error) {
	prompt := &directSSHAssetPrompt{
		user:        user,
		instruction: instruction,
		questions:   append([]string(nil), questions...),
		echos:       append([]bool(nil), echos...),
		answerCh:    make(chan directSSHAssetPromptAnswer, 1),
	}
	s.setCurrentPendingPrompt(prompt)
	select {
	case s.events <- directSSHAssetAuthEvent{prompt: prompt}:
	case <-s.ctxDone:
		s.setCurrentPendingPrompt(nil)
		return nil, errors.New("asset SSH authentication canceled")
	}

	select {
	case answer := <-prompt.answerCh:
		s.setCurrentPendingPrompt(nil)
		return answer.answers, answer.err
	case <-s.ctxDone:
		s.setCurrentPendingPrompt(nil)
		return nil, errors.New("asset SSH authentication canceled")
	}
}

func (s *directSSHAssetAuthState) waitForNextEvent() directSSHAssetAuthEvent {
	select {
	case event := <-s.events:
		return event
	case <-s.ctxDone:
		return directSSHAssetAuthEvent{err: errors.New("asset SSH authentication canceled")}
	}
}

func (s *directSSHAssetAuthState) currentPendingPrompt() *directSSHAssetPrompt {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.currentPrompt
}

func (s *directSSHAssetAuthState) setCurrentPendingPrompt(prompt *directSSHAssetPrompt) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.currentPrompt = prompt
}

func (s *directSSHAssetAuthState) takeCurrentPendingPrompt() *directSSHAssetPrompt {
	s.mu.Lock()
	defer s.mu.Unlock()
	prompt := s.currentPrompt
	s.currentPrompt = nil
	return prompt
}

func (s *directSSHAssetAuthState) submitPromptAnswers(answers []string, err error) error {
	prompt := s.takeCurrentPendingPrompt()
	if prompt == nil {
		return errors.New("asset keyboard-interactive prompt not found")
	}
	select {
	case prompt.answerCh <- directSSHAssetPromptAnswer{
		answers: append([]string(nil), answers...),
		err:     err,
	}:
		return nil
	case <-s.ctxDone:
		return errors.New("asset SSH authentication canceled")
	}
}

func storePreparedDirectSSHClient(ctx ssh.Context, prepared *srvconn.PreparedSSHClient) {
	ctx.SetValue(ContextKeyPreparedDirectSSHClient, prepared)
}

func GetPreparedDirectSSHClient(ctx context.Context,
	token *model.ConnectToken) *srvconn.PreparedSSHClient {
	prepared, _ := ctx.Value(ContextKeyPreparedDirectSSHClient).(*srvconn.PreparedSSHClient)
	if prepared == nil || !prepared.IsValidFor(token) {
		return nil
	}
	return prepared
}

func TakePreparedDirectSSHClient(ctx context.Context,
	token *model.ConnectToken) *srvconn.SSHClient {
	prepared, _ := ctx.Value(ContextKeyPreparedDirectSSHClient).(*srvconn.PreparedSSHClient)
	if prepared == nil {
		return nil
	}
	return prepared.TakeForToken(token)
}
