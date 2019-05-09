package sshd

import (
	"fmt"
	"strconv"
	"time"

	"github.com/gliderlabs/ssh"

	"cocogo/pkg/auth"
	"cocogo/pkg/config"
	"cocogo/pkg/handler"
	"cocogo/pkg/logger"
)

const version = "v1.4.0"

var (
	conf = config.Conf
)

func StartServer() {
	logger.Debug("Load host key")
	hostKey := HostKey{Value: conf.HostKey, Path: conf.HostKeyFile}
	signer, err := hostKey.Load()
	if err != nil {
		logger.Fatal("Load host key error: ", err)
	}
	fmt.Println(time.Now().Format("2006-01-02 15:04:05"))
	fmt.Printf("Coco version %s, more see https://www.jumpserver.org\n", version)
	fmt.Printf("Start ssh server at %s:%d\n", conf.BindHost, conf.SSHPort)
	fmt.Println("Quit the server with CONTROL-C.")

	srv := ssh.Server{
		Addr:                       conf.BindHost + ":" + strconv.Itoa(conf.SSHPort),
		PasswordHandler:            auth.CheckUserPassword,
		PublicKeyHandler:           auth.CheckUserPublicKey,
		KeyboardInteractiveHandler: auth.CheckMFA,
		NextAuthMethodsHandler:     auth.CheckUserNeedMFA,
		HostSigners:                []ssh.Signer{signer},
		Handler:                    handler.SessionHandler,
		SubsystemHandlers:          map[string]ssh.SubsystemHandler{},
	}
	srv.SetSubsystemHandler("sftp", handler.SftpHandler)
	logger.Fatal(srv.ListenAndServe())
}
