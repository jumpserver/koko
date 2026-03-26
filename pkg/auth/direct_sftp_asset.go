package auth

import (
	"errors"
	"strconv"
	"strings"
	"sync"

	"github.com/gliderlabs/ssh"
	gossh "golang.org/x/crypto/ssh"

	"github.com/jumpserver-dev/sdk-go/model"
	"github.com/jumpserver-dev/sdk-go/service"
	"github.com/jumpserver/koko/pkg/config"
	"github.com/jumpserver/koko/pkg/logger"
	"github.com/jumpserver/koko/pkg/srvconn"
)

type AuthPhase string

const (
	AuthPhaseNone                       AuthPhase = ""
	AuthPhaseUserKeyboardInteractive    AuthPhase = "user_keyboard_interactive"
	AuthPhaseDirectSFTPAssetInteractive AuthPhase = "direct_sftp_asset_keyboard_interactive"
)

type directSFTPAssetAuthState struct {
	Token     *model.ConnectToken
	ctxDone   <-chan struct{}
	sessionID string

	mu            sync.Mutex
	currentPrompt *directSFTPAssetPrompt
	events        chan directSFTPAssetAuthEvent
}

type directSFTPAssetAuthEvent struct {
	prompt   *directSFTPAssetPrompt
	prepared *srvconn.PreparedDirectSFTP
	err      error
}

type directSFTPAssetPrompt struct {
	User        string
	Instruction string
	Questions   []string
	Echos       []bool

	answerCh chan directSFTPAssetPromptAnswer
}

type directSFTPAssetPromptAnswer struct {
	answers []string
	err     error
}

func completeSSHAuth(ctx ssh.Context) error {
	ctx.SetValue(ContextKeyAuthPhase, AuthPhaseNone)
	return prepareDirectSFTPAssetIfNeeded(ctx)
}

func continueDirectSFTPAssetAuth(ctx ssh.Context, challenger gossh.KeyboardInteractiveChallenge) error {
	state, ok := ctx.Value(ContextKeyDirectSFTPAssetAuthState).(*directSFTPAssetAuthState)
	if !ok || state == nil || state.Token == nil {
		logger.Errorf("SSH conn[%s] direct sftp asset auth state not found", ctx.SessionID())
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
				storePreparedDirectSFTP(ctx, event.prepared)
				ctx.SetValue(ContextKeyDirectSFTPAssetAuthState, nil)
				ctx.SetValue(ContextKeyAuthPhase, AuthPhaseNone)
				return nil
			default:
				logger.Errorf("SSH conn[%s] direct sftp asset keyboard auth failed: %s",
					ctx.SessionID(), event.err)
				return authErr
			}
		}

		answers, err := challenger(prompt.User, prompt.Instruction, prompt.Questions, prompt.Echos)
		if submitErr := state.submitPromptAnswers(answers, err); submitErr != nil {
			logger.Errorf("SSH conn[%s] direct sftp submit client answers failed: %s", ctx.SessionID(), submitErr)
			return authErr
		}
		if err != nil {
			logger.Errorf("SSH conn[%s] direct sftp interactive client answer failed: %s", ctx.SessionID(), err)
			return authErr
		}
	}
}

func prepareDirectSFTPAssetIfNeeded(ctx ssh.Context) error {
	req, ok := ctx.Value(ContextKeyDirectLoginFormat).(*DirectLoginAssetReq)
	if !ok || req == nil {
		return nil
	}
	if req.Protocol != model.ProtocolSFTP {
		return nil
	}
	if prepared, ok := ctx.Value(ContextKeyPreparedDirectSFTP).(*srvconn.PreparedDirectSFTP); ok && prepared != nil && prepared.IsValid() {
		return nil
	}
	user, ok := ctx.Value(ContextKeyUser).(*model.User)
	if !ok || user == nil || user.ID == "" {
		logger.Errorf("SSH conn[%s] direct sftp auth user not found", ctx.SessionID())
		return authErr
	}
	jmsService, ok := ctx.Value(ContextKeyJMService).(*service.JMService)
	if !ok || jmsService == nil {
		logger.Errorf("SSH conn[%s] direct sftp auth jms service not found", ctx.SessionID())
		return authErr
	}
	token, err := BuildDirectConnectToken(ctx, jmsService, user, req)
	if err != nil {
		logger.Errorf("SSH conn[%s] build direct sftp token failed: %s",
			ctx.SessionID(), err)
		return authErr
	}
	state := newDirectSFTPAssetAuthState(ctx.SessionID(), token, ctx.Done())
	state.startHandshake()
	event := state.waitForNextEvent()
	switch {
	case event.prepared != nil:
		storePreparedDirectSFTP(ctx, event.prepared)
		ctx.SetValue(ContextKeyAuthPhase, AuthPhaseNone)
		return nil
	case event.prompt != nil:
		logger.Infof("SSH conn[%s] direct sftp prepare requires keyboard-interactive", ctx.SessionID())
		ctx.SetValue(ContextKeyDirectSFTPAssetAuthState, state)
		ctx.SetValue(ContextKeyAuthPhase, AuthPhaseDirectSFTPAssetInteractive)
		return &ssh.PartialSuccessError{Next: ssh.ServerAuthCallbacks{
			KeyboardInteractiveCallback: SSHKeyboardInteractiveAuth,
		}}
	default:
		logger.Errorf("SSH conn[%s] prepare direct sftp asset failed: %s",
			ctx.SessionID(), event.err)
		return authErr
	}
}

func buildDirectSFTPChallengeBridge(sessionID string, token *model.ConnectToken,
	challenger gossh.KeyboardInteractiveChallenge) gossh.KeyboardInteractiveChallenge {
	password := ""
	if token != nil && !token.Account.IsSSHKey() {
		password = token.Account.Secret
	}
	displayUser := ""
	if token != nil {
		displayUser = token.Account.Username
	}
	return func(user, instruction string, questions []string, echos []bool) (answers []string, err error) {
		if len(questions) == 0 {
			return []string{}, nil
		}

		answers = make([]string, len(questions))
		pendingQuestions := make([]string, 0, len(questions))
		pendingEchos := make([]bool, 0, len(questions))
		pendingIndexes := make([]int, 0, len(questions))
		for i := range questions {
			if password != "" && isPasswordQuestion(questions[i]) {
				answers[i] = password
				continue
			}
			echo := false
			if i < len(echos) {
				echo = echos[i]
			}
			pendingIndexes = append(pendingIndexes, i)
			pendingQuestions = append(pendingQuestions, questions[i])
			pendingEchos = append(pendingEchos, echo)
		}
		autoFilledCount := len(questions) - len(pendingIndexes)
		if len(pendingIndexes) == 0 {
			return answers, nil
		}

		challengeUser := user
		if challengeUser == "" {
			challengeUser = displayUser
		}
		logger.Infof("SSH conn[%s] direct sftp interactive forwarding %d/%d questions to client (auto-filled %d)",
			sessionID, len(pendingIndexes), len(questions), autoFilledCount)
		pendingAnswers, err := challenger(challengeUser, instruction, pendingQuestions, pendingEchos)
		if err != nil {
			logger.Errorf("SSH conn[%s] direct sftp interactive client challenge failed: %s",
				sessionID, err)
			return nil, err
		}
		if len(pendingAnswers) != len(pendingIndexes) {
			return nil, errors.New("direct sftp challenge answers mismatch")
		}
		for i, answer := range pendingAnswers {
			answers[pendingIndexes[i]] = answer
		}
		return answers, nil
	}
}

func newDirectSFTPAssetAuthState(sessionID string, token *model.ConnectToken, ctxDone <-chan struct{}) *directSFTPAssetAuthState {
	return &directSFTPAssetAuthState{
		Token:     token,
		ctxDone:   ctxDone,
		sessionID: sessionID,
		events:    make(chan directSFTPAssetAuthEvent, 1),
	}
}

func (s *directSFTPAssetAuthState) startHandshake() {
	go func() {
		prepared, err := newPreparedDirectSFTP(s.Token, srvconn.SSHClientKeyboardAuth(
			buildDirectSFTPChallengeBridge(s.sessionID, s.Token, s.awaitClientAnswers),
		))
		select {
		case s.events <- directSFTPAssetAuthEvent{prepared: prepared, err: err}:
		case <-s.ctxDone:
			if prepared != nil && prepared.Client != nil {
				_ = prepared.Client.Close()
			}
		}
	}()
}

func (s *directSFTPAssetAuthState) awaitClientAnswers(user, instruction string, questions []string, echos []bool) ([]string, error) {
	prompt := &directSFTPAssetPrompt{
		User:        user,
		Instruction: instruction,
		Questions:   append([]string(nil), questions...),
		Echos:       append([]bool(nil), echos...),
		answerCh:    make(chan directSFTPAssetPromptAnswer, 1),
	}
	s.setCurrentPendingPrompt(prompt)
	select {
	case s.events <- directSFTPAssetAuthEvent{prompt: prompt}:
	case <-s.ctxDone:
		s.setCurrentPendingPrompt(nil)
		return nil, errors.New("direct sftp auth canceled")
	}

	select {
	case answer := <-prompt.answerCh:
		s.setCurrentPendingPrompt(nil)
		return answer.answers, answer.err
	case <-s.ctxDone:
		s.setCurrentPendingPrompt(nil)
		return nil, errors.New("direct sftp auth canceled")
	}
}

func (s *directSFTPAssetAuthState) waitForNextEvent() directSFTPAssetAuthEvent {
	select {
	case event := <-s.events:
		return event
	case <-s.ctxDone:
		return directSFTPAssetAuthEvent{err: errors.New("direct sftp auth canceled")}
	}
}

func (s *directSFTPAssetAuthState) currentPendingPrompt() *directSFTPAssetPrompt {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.currentPrompt
}

func (s *directSFTPAssetAuthState) setCurrentPendingPrompt(prompt *directSFTPAssetPrompt) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.currentPrompt = prompt
}

func (s *directSFTPAssetAuthState) takeCurrentPendingPrompt() *directSFTPAssetPrompt {
	s.mu.Lock()
	defer s.mu.Unlock()
	prompt := s.currentPrompt
	s.currentPrompt = nil
	return prompt
}

func (s *directSFTPAssetAuthState) submitPromptAnswers(answers []string, err error) error {
	prompt := s.takeCurrentPendingPrompt()
	if prompt == nil {
		return errors.New("direct sftp prompt not found")
	}
	select {
	case prompt.answerCh <- directSFTPAssetPromptAnswer{answers: answers, err: err}:
		return nil
	case <-s.ctxDone:
		return errors.New("direct sftp auth canceled")
	}
}

func isPasswordQuestion(question string) bool {
	normalized := strings.ToLower(strings.TrimSpace(question))
	return strings.Contains(normalized, "password")
}

func newPreparedDirectSFTP(token *model.ConnectToken, extraOpts ...srvconn.SSHClientOption) (*srvconn.PreparedDirectSFTP, error) {
	sshClient, err := srvconn.NewSSHClient(buildSSHClientOptionsFromToken(token, extraOpts...)...)
	if err != nil {
		return nil, err
	}
	return &srvconn.PreparedDirectSFTP{
		Token:              token,
		Client:             sshClient,
		DisableIdleRecycle: true,
	}, nil
}

func storePreparedDirectSFTP(ctx ssh.Context, prepared *srvconn.PreparedDirectSFTP) {
	ctx.SetValue(ContextKeyPreparedDirectSFTP, prepared)
	go func(done <-chan struct{}, client *srvconn.SSHClient) {
		<-done
		_ = client.Close()
	}(ctx.Done(), prepared.Client)
}

func buildSSHClientOptionsFromToken(token *model.ConnectToken, extraOpts ...srvconn.SSHClientOption) []srvconn.SSHClientOption {
	asset := token.Asset
	account := token.Account
	timeout := config.GetConf().SSHTimeout
	sshAuthOpts := make([]srvconn.SSHClientOption, 0, 8+len(extraOpts))
	sshAuthOpts = append(sshAuthOpts, srvconn.SSHClientUsername(account.Username))
	sshAuthOpts = append(sshAuthOpts, srvconn.SSHClientHost(asset.Address))
	sshAuthOpts = append(sshAuthOpts, srvconn.SSHClientPort(asset.ProtocolPort(token.Protocol)))
	sshAuthOpts = append(sshAuthOpts, srvconn.SSHClientTimeout(timeout))
	if account.IsSSHKey() {
		if signer, err := gossh.ParsePrivateKey([]byte(account.Secret)); err == nil {
			sshAuthOpts = append(sshAuthOpts, srvconn.SSHClientPrivateAuth(signer))
		}
	} else {
		sshAuthOpts = append(sshAuthOpts, srvconn.SSHClientPassword(account.Secret))
	}
	if token.Gateway != nil {
		gateway := token.Gateway
		proxyArgs := make([]srvconn.SSHClientOptions, 0, 1)
		loginAccount := gateway.Account
		proxyArg := srvconn.SSHClientOptions{
			Host:     gateway.Address,
			Port:     strconv.Itoa(gateway.Protocols.GetProtocolPort(model.ProtocolSSH)),
			Username: loginAccount.Username,
			Timeout:  timeout,
		}
		if loginAccount.IsSSHKey() {
			proxyArg.PrivateKey = loginAccount.Secret
		} else {
			proxyArg.Password = loginAccount.Secret
		}
		proxyArgs = append(proxyArgs, proxyArg)
		sshAuthOpts = append(sshAuthOpts, srvconn.SSHClientProxyClient(proxyArgs...))
	}
	sshAuthOpts = append(sshAuthOpts, extraOpts...)
	return sshAuthOpts
}

func isPartialSuccess(err error) bool {
	var partialErr *ssh.PartialSuccessError
	return errors.As(err, &partialErr)
}
