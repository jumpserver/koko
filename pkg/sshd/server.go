package sshd

import (
	"net"

	"github.com/gliderlabs/ssh"

	"github.com/jumpserver/koko/pkg/auth"
	"github.com/jumpserver/koko/pkg/config"
	"github.com/jumpserver/koko/pkg/handler"
	"github.com/jumpserver/koko/pkg/logger"
)

var sshServer *ssh.Server

func StartServer() {
	conf := config.GetConf()
	hostKey := HostKey{Value: conf.HostKey, Path: conf.HostKeyFile}
	logger.Debug("Loading host key")
	signer, err := hostKey.Load()
	if err != nil {
		logger.Fatal("Load host key error: ", err)
	}

	addr := net.JoinHostPort(conf.BindHost, conf.SSHPort)
	logger.Infof("Start ssh server at %s", addr)
	sshServer = &ssh.Server{
		Addr:                       addr,
		KeyboardInteractiveHandler: auth.CheckMFA,
		PasswordHandler:            auth.CheckUserPassword,
		PublicKeyHandler:           auth.CheckUserPublicKey,
		NextAuthMethodsHandler:     auth.MFAAuthMethods,
		HostSigners:                []ssh.Signer{signer},
		Handler:                    handler.SessionHandler,
		SubsystemHandlers:          map[string]ssh.SubsystemHandler{},
	}
	// Set sftp handler
	sshServer.SetSubsystemHandler("sftp", handler.SftpHandler)
	logger.Fatal(sshServer.ListenAndServe())
}

func StopServer() {
	err := sshServer.Close()
	if err != nil {
		logger.Errorf("SSH server close failed: %s", err.Error())
	}
	logger.Debug("Close ssh server")
}
