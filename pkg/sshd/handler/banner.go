package handler

import (
	"cocogo/pkg/config"
	"fmt"
	"io"
	"text/template"

	"github.com/gliderlabs/ssh"

	"cocogo/pkg/logger"
)

const defaultTitle = `Welcome to use Jumpserver open source fortress system`

type MenuItem struct {
	instruct string
	helpText string
}

type Menu []MenuItem

var menu = Menu{
	{instruct: "ID", helpText: "directly login or enter."},
	{instruct: "part IP, Hostname, Comment", helpText: "to search login if unique."},
	{instruct: "/ + IP, Hostname, Comment", helpText: "to search, such as: /192.168"},
	{instruct: "p", helpText: "display the host you have permission."},
	{instruct: "g", helpText: "display the node that you have permission."},
	{instruct: "r", helpText: "refresh your assets and nodes"},
	{instruct: "s", helpText: "switch Chinese-english language."},
	{instruct: "h", helpText: "print help"},
	{instruct: "q", helpText: "exit"},
}

type Banner struct {
	user string
}

type ColorMeta struct {
	GreenBoldColor string
	ColorEnd       string
}

func displayBanner(sess ssh.Session, user string) {
	title := defaultTitle
	if config.Conf.HeaderTitle != "" {
		title = config.Conf.HeaderTitle
	}
	welcomeMsg := user + "  " + title
	_, err := io.WriteString(sess, welcomeMsg)
	if err != nil {
		logger.Error("Send to client error, %s", err)
	}
	cm := ColorMeta{GreenBoldColor: "\033[1;32m", ColorEnd: "\033[0m"}
	for i, v := range menu {
		line := fmt.Sprintf("\t%d) Enter {{.GreenBoldColor}}%s{{.ColorEnd}} to %s.\r\n", i+1, v.instruct, v.helpText)
		tmpl := template.Must(template.New("item").Parse(line))
		err := tmpl.Execute(sess, cm)
		if err != nil {
			logger.Error("Send to client error, %s", err)
		}
	}
}
