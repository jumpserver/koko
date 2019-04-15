package sshd

import (
	uuid "github.com/satori/go.uuid"

	gossh "golang.org/x/crypto/ssh"
)

func parsePrivateKey(privateKey string) gossh.Signer {
	private, err := gossh.ParsePrivateKey([]byte(privateKey))
	if err != nil {
		log.Info("Failed to parse private key: ", err)
	}
	return private
}

func generateNewUUID() uuid.UUID {
	return uuid.NewV4()
}
