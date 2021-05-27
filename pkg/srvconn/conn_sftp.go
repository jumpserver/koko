package srvconn

import (
	"github.com/pkg/sftp"
	gossh "golang.org/x/crypto/ssh"
)

func NewSftpConn(sess *gossh.Session) (*sftp.Client, error) {
	if err := sess.RequestSubsystem("sftp"); err != nil {
		return nil, err
	}
	pw, err := sess.StdinPipe()
	if err != nil {
		return nil, err
	}
	pr, err := sess.StdoutPipe()
	if err != nil {
		return nil, err
	}
	client, err := sftp.NewClientPipe(pr, pw)
	if err != nil {
		return nil, err
	}
	return client, err
}
