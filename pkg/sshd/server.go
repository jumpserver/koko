package sshd

import (
	"strconv"

	"github.com/gliderlabs/ssh"

	"cocogo/pkg/auth"
	"cocogo/pkg/config"
	"cocogo/pkg/handler"
	"cocogo/pkg/logger"
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

	logger.Infof("Start ssh server at %s:%d", conf.BindHost, conf.SSHPort)
	sshServer = &ssh.Server{
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
	sshServer.SetSubsystemHandler("sftp", handler.SftpHandler)
	logger.Fatal(sshServer.ListenAndServe())
}

func StopServer() {
	err := sshServer.Close()
	if err != nil {
		logger.Debugf("ssh server close failed: %s", err.Error())
	}
	logger.Debug("Close ssh Server")

}
