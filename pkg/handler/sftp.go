package handler

import (
	"io"
	"io/ioutil"

	"github.com/gliderlabs/ssh"
	"github.com/pkg/sftp"

	"cocogo/pkg/logger"
)

func SftpHandler(sess ssh.Session) {
	debugStream := ioutil.Discard
	serverOptions := []sftp.ServerOption{
		sftp.WithDebug(debugStream),
	}
	server, err := sftp.NewServer(
		sess,
		serverOptions...,
	)
	if err != nil {
		logger.Errorf("sftp server init error: %s", err)
		return
	}
	if err := server.Serve(); err == io.EOF {
		server.Close()
		logger.Info("sftp client exited session.")
	} else if err != nil {
		logger.Errorf("sftp server completed with error:", err)
	}
}
