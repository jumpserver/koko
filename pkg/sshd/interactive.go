package sshd

import (
	"cocogo/pkg/asset"
	"io"
	"strings"

	"github.com/gliderlabs/ssh"
	"golang.org/x/crypto/ssh/terminal"
)

const welcomeTemplate = `
		{{.UserName}}	Welcome to use Jumpserver open source fortress system
{{.Tab}}1) Enter {{.ColorCode}}ID{{.ColorEnd}} directly login or enter {{.ColorCode}}part IP, Hostname, Comment{{.ColorEnd}} to search login(if unique). {{.EndLine}}
{{.Tab}}2) Enter {{.ColorCode}}/{{.ColorEnd}} + {{.ColorCode}}IP, Hostname{{.ColorEnd}} or {{.ColorCode}}Comment{{.ColorEnd}} search, such as: /ip. {{.EndLine}}
{{.Tab}}3) Enter {{.ColorCode}}p{{.ColorEnd}} to display the host you have permission.{{.EndLine}}
{{.Tab}}4) Enter {{.ColorCode}}g{{.ColorEnd}} to display the node that you have permission.{{.EndLine}}
{{.Tab}}5) Enter {{.ColorCode}}g{{.ColorEnd}} + {{.ColorCode}}NodeID{{.ColorEnd}} to display the host under the node, such as g1. {{.EndLine}}
{{.Tab}}6) Enter {{.ColorCode}}s{{.ColorEnd}} Chinese-english switch.{{.EndLine}}
{{.Tab}}7) Enter {{.ColorCode}}h{{.ColorEnd}} help.{{.EndLine}}
{{.Tab}}8) Enter {{.ColorCode}}r{{.ColorEnd}} to refresh your assets and nodes.{{.EndLine}}
{{.Tab}}0) Enter {{.ColorCode}}q{{.ColorEnd}} exit.{{.EndLine}}
`

type HelpInfo struct {
	UserName  string
	ColorCode string
	ColorEnd  string
	Tab       string
	EndLine   string
}

func (d HelpInfo) displayHelpInfo(sess ssh.Session) {
	e := displayTemplate.Execute(sess, d)
	if e != nil {
		log.Warn("display help info failed")
	}
}

func InteractiveHandler(sess ssh.Session) {
	_, _, ptyOk := sess.Pty()
	if ptyOk {

		helpInfo := HelpInfo{
			UserName:  sess.User(),
			ColorCode: "\033[32m",
			ColorEnd:  "\033[0m",
			Tab:       "\t",
			EndLine:   "\r\n\r",
		}

		log.Info("accept one session")
		helpInfo.displayHelpInfo(sess)
		term := terminal.NewTerminal(sess, "Opt>")
		for {
			line, err := term.ReadLine()
			if err != nil {
				log.Error(err)
				break
			}
			switch line {
			case "p", "P":
				_, err := io.WriteString(sess, "p cmd execute\r\n")
				if err != nil {
					return
				}
			case "g", "G":
				_, err := io.WriteString(sess, "g cmd execute\r\n")
				if err != nil {
					return
				}
			case "s", "S":
				_, err := io.WriteString(sess, "s cmd execute\r\n")
				if err != nil {
					return
				}
			case "h", "H":
				helpInfo.displayHelpInfo(sess)

			case "r", "R":
				_, err := io.WriteString(sess, "r cmd execute\r\n")
				if err != nil {
					return
				}
			case "q", "Q", "exit", "quit":
				log.Info("exit session")
				return
			default:
				searchNodeAndProxy(line, sess)
			}
		}

	} else {
		_, err := io.WriteString(sess, "No PTY requested.\n")
		if err != nil {
			return
		}
	}

}

func searchNodeAndProxy(line string, sess ssh.Session) {
	searchKey := strings.TrimPrefix(line, "/")
	if node, ok := searchNode(searchKey); ok {
		err := Proxy(sess, node)
		if err != nil {
			log.Info("proxy err ", err)
		}
	}
}

func searchNode(key string) (asset.Node, bool) {
	if key == "docker" {
		return asset.Node{
			IP:       "127.0.0.1",
			Port:     "32768",
			UserName: "root",
			PassWord: "screencast",
		}, true
	}
	return asset.Node{}, false
}
