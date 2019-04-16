package sshd

import (
	gossh "golang.org/x/crypto/ssh"
)

func parsePrivateKey(privateKey string) gossh.Signer {
	private, err := gossh.ParsePrivateKey([]byte(privateKey))
	if err != nil {
		log.Error("Failed to parse private key: ", err)
	}
	return private
}
