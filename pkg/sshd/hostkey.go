package sshd

import (
	"golang.org/x/crypto/ssh"
	"io/ioutil"
	"os"
)

type HostKey struct {
	Value string
	Path  string
}

func (hk *HostKey) loadHostKeyFromFile(keyPath string) (signer ssh.Signer, err error) {
	_, err = os.Stat(conf.HostKeyFile)
	if err != nil {
		return
	}
	buf, err := ioutil.ReadFile(conf.HostKeyFile)
	if err != nil {
		return
	}
	return hk.loadHostKeyFromString(string(buf))
}

func (hk *HostKey) loadHostKeyFromString(value string) (signer ssh.Signer, err error) {
	signer, err = ssh.ParsePrivateKey([]byte(value))
	return
}

func (hk *HostKey) Gen() (signer ssh.Signer, err error) {
	return
}

func (hk *HostKey) SaveToFile(signer ssh.Signer) (err error) {
	return
}

func (hk *HostKey) Load() (signer ssh.Signer, err error) {
	if hk.Value != "" {
		signer, err = hk.loadHostKeyFromString(hk.Value)
		if err == nil {
			return
		}
	}
	if hk.Path != "" {
		signer, err = hk.loadHostKeyFromFile(hk.Path)
		if err == nil {
			return
		}
	}
	signer, err = hk.Gen()
	if err != nil {
		return
	}
	err = hk.SaveToFile(signer)
	if err != nil {
		return
	}
	return
}
