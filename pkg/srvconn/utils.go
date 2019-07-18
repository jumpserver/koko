package srvconn

import (
	"io/ioutil"

	"golang.org/x/crypto/ssh"
)

func GetPubKeyFromFile(keypath string) (ssh.Signer, error) {
	buf, err := ioutil.ReadFile(keypath)
	if err != nil {
		return nil, err
	}

	pubkey, err := ssh.ParsePrivateKey(buf)
	if err != nil {
		return nil, err
	}

	return pubkey, nil
}
