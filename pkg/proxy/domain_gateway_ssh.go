package proxy

import (
	"errors"
	"net"
	"strconv"
	"time"

	"github.com/jumpserver/koko/pkg/common"
	"github.com/jumpserver/koko/pkg/config"
	"github.com/jumpserver/koko/pkg/logger"
	gossh "golang.org/x/crypto/ssh"

	"github.com/jumpserver-dev/sdk-go/model"
)

var errNilGateway = errors.New("gateway is nil")

func newGatewaySSHClient(gateway *model.Gateway) (*gossh.Client, error) {
	if gateway == nil {
		return nil, errNilGateway
	}

	configTimeout := time.Duration(config.GetConf().SSHTimeout)
	auths := make([]gossh.AuthMethod, 0, 3)
	loginAccount := gateway.Account
	if loginAccount.IsSSHKey() {
		if signer, err := gossh.ParsePrivateKey([]byte(loginAccount.Secret)); err == nil {
			auths = append(auths, gossh.PublicKeys(signer))
		} else {
			logger.Errorf("Domain gateway parse private key error: %s", err)
		}
	} else {
		auths = append(auths, gossh.Password(loginAccount.Secret))
		auths = append(auths, gossh.KeyboardInteractive(func(user, instruction string,
			questions []string, echos []bool) (answers []string, err error) {
			return []string{loginAccount.Secret}, nil
		}))
	}

	sshConfig := gossh.ClientConfig{
		User:            loginAccount.Username,
		Auth:            auths,
		HostKeyCallback: common.NewTrustHostKeyCallback(),
		Timeout:         configTimeout * time.Second,
	}
	port := gateway.Protocols.GetProtocolPort(model.ProtocolSSH)
	addr := net.JoinHostPort(gateway.Address, strconv.Itoa(port))
	return gossh.Dial("tcp", addr, &sshConfig)
}
