package sshd

import (
	"golang.org/x/crypto/ssh"
)

func ParsePrivateKeyFromString(content string) (signer ssh.Signer, err error) {
	return ssh.ParsePrivateKey([]byte(content))
}

func ParsePrivateKeyWithPassphrase(privateKey, Passphrase string) (signer ssh.Signer, err error) {
	return ssh.ParsePrivateKeyWithPassphrase([]byte(privateKey), []byte(Passphrase))
}
