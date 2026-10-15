package srvconn

import (
	"github.com/pkg/sftp"
	gossh "golang.org/x/crypto/ssh"
)

// pkg/sftp reads concurrently by default but writes serially, so a 2MB WriteAt
// becomes 64 sequential 32KB round-trips. Partial writes stay safe here because
// transfers verify SHA256 before the staging file is renamed.
func sftpClientOptions() []sftp.ClientOption {
	return []sftp.ClientOption{sftp.UseConcurrentWrites(true)}
}

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
	client, err := sftp.NewClientPipe(pr, pw, sftpClientOptions()...)
	if err != nil {
		return nil, err
	}
	return client, err
}
