package auth

import (
	"errors"
	"strconv"
	"strings"

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
	Token *model.ConnectToken
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
	prepared, err := newPreparedDirectSFTP(state.Token, srvconn.SSHClientKeyboardAuth(
		buildDirectSFTPChallengeBridge(state.Token, challenger),
	))
	if err != nil {
		logger.Errorf("SSH conn[%s] direct sftp asset keyboard auth failed: %s", ctx.SessionID(), err)
		return authErr
	}
	storePreparedDirectSFTP(ctx, prepared)
	ctx.SetValue(ContextKeyDirectSFTPAssetAuthState, nil)
	ctx.SetValue(ContextKeyAuthPhase, AuthPhaseNone)
	return nil
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
		logger.Errorf("SSH conn[%s] build direct sftp token failed: %s", ctx.SessionID(), err)
		return authErr
	}
	detector := &directSFTPChallengeDetector{password: token.Account.Secret}
	prepared, err := newPreparedDirectSFTP(token, srvconn.SSHClientKeyboardAuth(detector.challenge))
	if err == nil {
		storePreparedDirectSFTP(ctx, prepared)
		ctx.SetValue(ContextKeyAuthPhase, AuthPhaseNone)
		return nil
	}
	if detector.needInteractive {
		ctx.SetValue(ContextKeyDirectSFTPAssetAuthState, &directSFTPAssetAuthState{Token: token})
		ctx.SetValue(ContextKeyAuthPhase, AuthPhaseDirectSFTPAssetInteractive)
		return &ssh.PartialSuccessError{Next: ssh.ServerAuthCallbacks{
			KeyboardInteractiveCallback: SSHKeyboardInteractiveAuth,
		}}
	}
	logger.Errorf("SSH conn[%s] prepare direct sftp asset failed: %s", ctx.SessionID(), err)
	return authErr
}

type directSFTPChallengeDetector struct {
	password        string
	needInteractive bool
}

func (d *directSFTPChallengeDetector) challenge(user, instruction string, questions []string, echos []bool) (answers []string, err error) {
	if len(questions) == 0 {
		return []string{}, nil
	}
	answers = make([]string, len(questions))
	for i := range questions {
		if isPasswordQuestion(questions[i]) && d.password != "" {
			answers[i] = d.password
			continue
		}
		d.needInteractive = true
	}
	return answers, nil
}

func buildDirectSFTPChallengeBridge(token *model.ConnectToken,
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
		if len(pendingIndexes) == 0 {
			return answers, nil
		}

		challengeUser := user
		if challengeUser == "" {
			challengeUser = displayUser
		}
		pendingAnswers, err := challenger(challengeUser, instruction, pendingQuestions, pendingEchos)
		if err != nil {
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
	timeout := config.GlobalConfig.SSHTimeout
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
