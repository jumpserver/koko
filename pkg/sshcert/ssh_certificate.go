package sshcert

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"fmt"
	"strings"

	gossh "golang.org/x/crypto/ssh"

	"github.com/jumpserver-dev/sdk-go/model"
	"github.com/jumpserver-dev/sdk-go/service"
)

var (
	errNotSSHCertificate = errors.New("account is not an SSH certificate account")
	errInvalidPrivateKey = errors.New("invalid ephemeral SSH private key")
)

// GetConnectTokenInfo creates a fresh in-memory key pair for every credential
// request. Core ignores the public key for non-certificate accounts, and the
// private key is retained only when Core returns an SSH certificate.
func GetConnectTokenInfo(jmsService *service.JMService, tokenID string,
	expireNow bool) (model.ConnectToken, error) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return model.ConnectToken{}, fmt.Errorf("generate ephemeral SSH key: %w", err)
	}

	clearBytes := func(value []byte) {
		for i := range value {
			value[i] = 0
		}
	}

	sshPublicKey, err := gossh.NewPublicKey(publicKey)
	if err != nil {
		clearBytes(privateKey)
		return model.ConnectToken{}, fmt.Errorf("encode ephemeral SSH public key: %w", err)
	}
	authorizedKey := strings.TrimSpace(string(gossh.MarshalAuthorizedKey(sshPublicKey)))

	connectToken, err := jmsService.GetConnectTokenInfoWithPublicKey(
		tokenID, expireNow, authorizedKey,
	)
	if err != nil {
		clearBytes(privateKey)
		return connectToken, err
	}
	if connectToken.Account.IsSSHCertificate() {
		connectToken.Account.SSHCertificatePrivateKey = privateKey
	} else {
		clearBytes(privateKey)
	}
	return connectToken, nil
}

// NewSigner combines the short-lived OpenSSH user certificate returned by
// Core with the process-local private key generated for the same request.
func NewSigner(account *model.BaseAccount) (gossh.Signer, error) {
	if account == nil || !account.IsSSHCertificate() {
		return nil, errNotSSHCertificate
	}
	if len(account.SSHCertificatePrivateKey) != ed25519.PrivateKeySize {
		return nil, errInvalidPrivateKey
	}

	privateSigner, err := gossh.NewSignerFromKey(
		ed25519.PrivateKey(account.SSHCertificatePrivateKey),
	)
	if err != nil {
		return nil, fmt.Errorf("parse ephemeral SSH private key: %w", err)
	}
	publicKey, _, _, _, err := gossh.ParseAuthorizedKey([]byte(account.Secret))
	if err != nil {
		return nil, fmt.Errorf("parse SSH certificate: %w", err)
	}
	certificate, ok := publicKey.(*gossh.Certificate)
	if !ok || certificate.CertType != gossh.UserCert {
		return nil, errors.New("Core returned an invalid SSH user certificate")
	}
	if !bytes.Equal(certificate.Key.Marshal(), privateSigner.PublicKey().Marshal()) {
		return nil, errors.New("SSH certificate does not match the ephemeral private key")
	}

	certificateSigner, err := gossh.NewCertSigner(certificate, privateSigner)
	if err != nil {
		return nil, fmt.Errorf("create SSH certificate signer: %w", err)
	}
	return certificateSigner, nil
}
