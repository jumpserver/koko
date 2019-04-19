package sshd

import (
	"cocogo/pkg/auth"
	"cocogo/pkg/config"
	"cocogo/pkg/model"
	"io"
	"strconv"
	"sync"
	"text/template"

	"golang.org/x/crypto/ssh/terminal"

	"github.com/gliderlabs/ssh"
	"github.com/sirupsen/logrus"
)

var (
	conf            *config.Config
	appService      *auth.Service
	serverSig       ssh.Signer
	displayTemplate *template.Template
	log             *logrus.Logger

	Cached *sync.Map
)

func Initial() {
	displayTemplate = template.Must(template.New("display").Parse(welcomeTemplate))
	Cached = new(sync.Map)
	conf = config.GetGlobalConfig()
	appService = auth.GetGlobalService()
	serverSig = parsePrivateKey(conf.TermConfig.HostKey)

	log = logrus.New()

	if level, err := logrus.ParseLevel(conf.LogLevel); err != nil {
		log.SetLevel(logrus.InfoLevel)
	} else {
		log.SetLevel(level)
	}

}

func StartServer() {
	srv := ssh.Server{
		Addr:             conf.BindHost + ":" + strconv.Itoa(conf.SshPort),
		PasswordHandler:  appService.CheckSSHPassword,
		PublicKeyHandler: appService.CheckSSHPublicKey,
		HostSigners:      []ssh.Signer{serverSig},
		Version:          "coco-v1.4",
		Handler:          connectHandler,
	}
	log.Fatal(srv.ListenAndServe())
}

func connectHandler(sess ssh.Session) {
	_, _, ptyOk := sess.Pty()
	if ptyOk {
		user, ok := sess.Context().Value("LoginUser").(model.User)
		if !ok {
			log.Info("Get current User failed")
			return
		}

		userInteractive := &sshInteractive{
			sess:      sess,
			term:      terminal.NewTerminal(sess, "Opt>"),
			user:      user,
			assetData: new(sync.Map),
			helpInfo: HelpInfo{UserName: sess.User(),
				ColorCode: GreenColorCode,
				ColorEnd:  ColorEnd,
				Tab:       Tab,
				EndLine:   EndLine}}

		log.Info("accept one session")
		userInteractive.displayHelpInfo()
		userInteractive.StartDispatch()

	} else {
		_, err := io.WriteString(sess, "No PTY requested.\n")
		if err != nil {
			return
		}
	}

}
