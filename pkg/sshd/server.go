package sshd

import (
	"strconv"

	"github.com/gliderlabs/ssh"

	"cocogo/pkg/auth"
	"cocogo/pkg/config"
	"cocogo/pkg/handler"
	"cocogo/pkg/logger"
)

func StartServer() {
	conf := config.GetConf()
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
		PasswordHandler:            auth.CheckUserPassword,
		PublicKeyHandler:           auth.CheckUserPublicKey,
		NextAuthMethodsHandler:     auth.MFAAuthMethods,
		HostSigners:                []ssh.Signer{signer},
		Handler:                    handler.SessionHandler,
		SubsystemHandlers:          map[string]ssh.SubsystemHandler{},
	}
	// Set Auth Handler
	srv.SetSubsystemHandler("sftp", handler.SftpHandler)
	logger.Fatal(srv.ListenAndServe())
}
