package handler

import (
	"bytes"
	"fmt"
	"io"
	"text/template"

	"github.com/jumpserver/koko/pkg/i18n"
	"github.com/jumpserver/koko/pkg/jms-sdk-go/model"
	"github.com/jumpserver/koko/pkg/logger"
	"github.com/jumpserver/koko/pkg/utils"
)

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
	mi.showText = buf.String()
	return mi.showText
}

type Menu []MenuItem

type ColorMeta struct {
	GreenBoldColor string
	ColorEnd       string
}

func (h *InteractiveHandler) displayBanner(sess io.ReadWriter, user string, termConf *model.TerminalConfig) {
	lang := i18n.NewLang(h.i18nLang)
	defaultTitle := utils.WrapperTitle(lang.T("Welcome to use JumpServer open source fortress system"))
	menu := Menu{
		{id: 1, instruct: lang.T("part IP, Hostname, Comment"), helpText: lang.T("to search login if unique")},
		{id: 2, instruct: lang.T("/ + IP, Hostname, Comment"), helpText: lang.T("to search, such as: /192.168")},
		{id: 3, instruct: "p", helpText: lang.T("display the host you have permission")},
		{id: 4, instruct: "g", helpText: lang.T("display the node that you have permission")},
		{id: 5, instruct: "d", helpText: lang.T("display the databases that you have permission")},
		{id: 6, instruct: "k", helpText: lang.T("display the kubernetes that you have permission")},
		{id: 7, instruct: "r", helpText: lang.T("refresh your assets and nodes")},
		{id: 8, instruct: "s", helpText: lang.T("Chinese-english switch")},
		{id: 9, instruct: "h", helpText: lang.T("print help")},
		{id: 10, instruct: "q", helpText: lang.T("exit")},
	}

	title := defaultTitle
	if termConf.HeaderTitle != "" {
		title = termConf.HeaderTitle
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
