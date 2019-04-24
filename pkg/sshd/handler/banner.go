package handler

import (
	"bytes"
	"cocogo/pkg/config"
	"fmt"
	"io"
	"text/template"

	"github.com/gliderlabs/ssh"

	"cocogo/pkg/logger"
)

const defaultTitle = `Welcome to use Jumpserver open source fortress system`

type MenuItem struct {
	id       int
	instruct string
	helpText string
	showText string
}

func (mi *MenuItem) Text() string {
	if mi.showText != "" {
		return mi.showText
	}
	cm := ColorMeta{GreenBoldColor: "\033[1;32m", ColorEnd: "\033[0m"}
	line := fmt.Sprintf("\t%d) Enter {{.GreenBoldColor}}%s{{.ColorEnd}} to %s.\r\n", mi.id, mi.instruct, mi.helpText)
	tmpl := template.Must(template.New("item").Parse(line))
	var buf bytes.Buffer
	err := tmpl.Execute(&buf, cm)
	if err != nil {
		logger.Error(err)
	}
	mi.showText = string(buf.Bytes())
	return mi.showText
}

type Menu []MenuItem

var menu = Menu{
	{id: 1, instruct: "ID", helpText: "directly login or enter"},
	{id: 2, instruct: "part IP, Hostname, Comment", helpText: "to search login if unique"},
	{id: 3, instruct: "/ + IP, Hostname, Comment", helpText: "to search, such as: /192.168"},
	{id: 4, instruct: "p", helpText: "display the host you have permission"},
	{id: 5, instruct: "g", helpText: "display the node that you have permission"},
	{id: 6, instruct: "r", helpText: "refresh your assets and nodes"},
	{id: 7, instruct: "s", helpText: "switch Chinese-english language"},
	{id: 8, instruct: "h", helpText: "print help"},
	{id: 9, instruct: "q", helpText: "exit"},
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
	welcomeMsg := CharClear + CharTab + user + "  " + title + CharNewLine
	_, err := io.WriteString(sess, welcomeMsg)
	if err != nil {
		logger.Error("Send to client error, %s", err)
	}
	for _, v := range menu {
		_, _ = io.WriteString(sess, v.Text())
	}
}
