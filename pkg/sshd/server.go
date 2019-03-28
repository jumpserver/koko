package sshd

import (
	"cocogo/pkg/auth"
	"strconv"
	"sync"
	"text/template"

	"github.com/sirupsen/logrus"

	"github.com/gliderlabs/ssh"
)

var (
	SSHPort          int
	SSHKeyPath       string
	log              *logrus.Logger
	displayTemplate  *template.Template
	authService      *auth.Service
	sessionContainer sync.Map
)

func init() {
	log = logrus.New()
	displayTemplate = template.Must(template.New("display").Parse(welcomeTemplate))
	SSHPort = 2333
	SSHKeyPath = "data/host_rsa_key"
	authService = auth.NewService()
}

func StartServer() {

	serverSig := getPrivateKey(SSHKeyPath)
	ser := ssh.Server{
		Addr:            "0.0.0.0:" + strconv.Itoa(SSHPort),
		PasswordHandler: authService.SSHPassword,
		HostSigners:     []ssh.Signer{serverSig},
		Version:         "coco-v1.4",
		Handler:         InteractiveHandler,
	}
	log.Fatal(ser.ListenAndServe())
}
