package sshd

import (
	"strconv"

	"github.com/gliderlabs/ssh"

	"cocogo/pkg/auth"
	"cocogo/pkg/config"
	"cocogo/pkg/handler"
	"cocogo/pkg/logger"
)

var conf = config.Conf

func StartServer() {
	hostKey := HostKey{Value: conf.HostKey, Path: conf.HostKeyFile}

	logger.Debug("Loading host key")
	signer, err := hostKey.Load()
	if err != nil {
		logger.Fatal("Load host key error: ", err)
	}

	logger.Infof("Start ssh server at %s:%d", conf.BindHost, conf.SSHPort)
	srv := ssh.Server{
		Addr:                       conf.BindHost + ":" + strconv.Itoa(conf.SSHPort),
		KeyboardInteractiveHandler: auth.CheckMFA,
		NextAuthMethodsHandler:     auth.CheckUserNeedMFA,
		HostSigners:                []ssh.Signer{signer},
		Handler:                    handler.SessionHandler,
		SubsystemHandlers:          map[string]ssh.SubsystemHandler{},
	}
	// Set Auth Handler
	if conf.PasswordAuth {
		srv.PasswordHandler = auth.CheckUserPassword
	}
	if conf.PublicKeyAuth {
		srv.PublicKeyHandler = auth.CheckUserPublicKey
	}
	if !conf.PasswordAuth && !conf.PublicKeyAuth {
		srv.PasswordHandler = auth.CheckUserPassword
	}
	srv.SetSubsystemHandler("sftp", handler.SftpHandler)
	logger.Fatal(srv.ListenAndServe())
}
