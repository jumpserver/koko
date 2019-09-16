package handler

import (
	"bytes"
	"fmt"
	"io"
	"text/template"

	"github.com/jumpserver/koko/pkg/config"
	"github.com/jumpserver/koko/pkg/i18n"
	"github.com/jumpserver/koko/pkg/logger"
	"github.com/jumpserver/koko/pkg/utils"
)

var defaultTitle string
var menu Menu

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
	line := fmt.Sprintf(i18n.T("\t%d) Enter {{.GreenBoldColor}}%s{{.ColorEnd}} to %s.%s"), mi.id, mi.instruct, mi.helpText, "\r\n")
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

func Initial() {
	defaultTitle = utils.WrapperTitle(i18n.T("Welcome to use Jumpserver open source fortress system"))
	menu = Menu{
		{id: 1, instruct: "ID", helpText: i18n.T("directly login")},
		{id: 2, instruct: i18n.T("part IP, Hostname, Comment"), helpText: i18n.T("to search login if unique")},
		{id: 3, instruct: i18n.T("/ + IP, Hostname, Comment"), helpText: i18n.T("to search, such as: /192.168")},
		{id: 4, instruct: "p", helpText: i18n.T("display the host you have permission")},
		{id: 5, instruct: "g", helpText: i18n.T("display the node that you have permission")},
		{id: 6, instruct: "r", helpText: i18n.T("refresh your assets and nodes")},
		{id: 7, instruct: "h", helpText: i18n.T("print help")},
		{id: 8, instruct: "q", helpText: i18n.T("exit")},
	}
}

type ColorMeta struct {
	GreenBoldColor string
	ColorEnd       string
}

func displayBanner(sess io.ReadWriter, user string) {
	title := defaultTitle
	cf := config.GetConf()
	if cf.HeaderTitle != "" {
		title = cf.HeaderTitle
	}

	prefix := utils.CharClear + utils.CharTab + utils.CharTab
	suffix := utils.CharNewLine + utils.CharNewLine
	welcomeMsg := prefix + utils.WrapperTitle(user+",") + "  " + title + suffix
	_, err := io.WriteString(sess, welcomeMsg)
	if err != nil {
		logger.Errorf("Send to client error, %s", err)
		return
	}
	for _, v := range menu {
		utils.IgnoreErrWriteString(sess, v.Text())
	}
}
