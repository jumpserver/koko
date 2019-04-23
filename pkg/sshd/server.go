package sshd

import (
	"strconv"

	"github.com/gliderlabs/ssh"

	"cocogo/pkg/auth"
	"cocogo/pkg/config"
	"cocogo/pkg/logger"

	"./handler"
)

var (
	conf = config.Conf
)

func StartServer() {
	logger.Debug("Load host access key")
	hostKey := HostKey{Value: conf.HostKey, Path: conf.HostKeyFile}
	signer, err := hostKey.Load()
	if err != nil {
		logger.Fatal("Load access key error: %s", err)
	}

	srv := ssh.Server{
		Addr:                       conf.BindHost + ":" + strconv.Itoa(conf.SSHPort),
		PasswordHandler:            auth.CheckUserPassword,
		PublicKeyHandler:           auth.CheckUserPublicKey,
		KeyboardInteractiveHandler: auth.CheckMFA,
		HostSigners:                []ssh.Signer{signer},
		Version:                    "coco-v1.4",
		Handler:                    handler.TestHandler,
	}
	logger.Fatal(srv.ListenAndServe())
}
