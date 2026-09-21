package handler

import (
	"fmt"
	"io"
	"strings"
	"text/template"
	"time"

	"github.com/jumpserver-dev/sdk-go/model"
	"github.com/jumpserver/koko/pkg/i18n"
	"github.com/jumpserver/koko/pkg/logger"
	"github.com/jumpserver/koko/pkg/utils"
)

type MenuItem struct {
	instruct string
	helpText string
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
		{instruct: lang.T("/ + IP, Hostname, Comment"), helpText: lang.T("to search, such as: /192.168")},
		{instruct: "p", helpText: lang.T("display the assets you have permission")},
		{instruct: "g", helpText: lang.T("authorization tree")},
		{instruct: "c", helpText: lang.T("type tree")},
		{instruct: "f", helpText: lang.T("favorite tree")},
		{instruct: "s", helpText: lang.T("language switch")},
		{instruct: "t", helpText: h.tr("切换到 TUI 模式", "switch to TUI mode")},
		{instruct: "?", helpText: lang.T("print help")},
		{instruct: "q", helpText: lang.T("exit")},
	}

	title := defaultTitle
	if termConf.HeaderTitle != "" {
		title = termConf.HeaderTitle
	}

	prefix := utils.CharClear + utils.CharTab + utils.CharTab
	suffix := utils.CharNewLine + utils.CharNewLine
	welcomeMsg := prefix + utils.WrapperTitle(user+",") + "  " + title + suffix
	if _, err := io.WriteString(sess, welcomeMsg); err != nil {
		logger.Errorf("Send to client error, %s", err)
		return
	}
	cm := ColorMeta{GreenBoldColor: "\033[1;32m", ColorEnd: "\033[0m"}
	lineFormat := alignClassicMenuIndex(lang.T("\t%2d) Enter {{.GreenBoldColor}}%s{{.ColorEnd}} to %s%s"))
	for i, item := range menu {
		line := fmt.Sprintf(lineFormat, i+1, item.instruct, item.helpText, "\r\n")
		tmpl := template.Must(template.New("item").Parse(line))
		if err := tmpl.Execute(sess, cm); err != nil {
			logger.Error(err)
		}
	}
}

func alignClassicMenuIndex(format string) string {
	return strings.Replace(format, "%d)", "%2d)", 1)
}

func (h *InteractiveHandler) displayAnnouncement(sess io.ReadWriter, setting *model.PublicSetting) {
	if !setting.EnableAnnouncement {
		return
	}
	if setting.Announcement.Subject == "" && setting.Announcement.Content == "" {
		return
	}
	now := time.Now()
	if now.Before(setting.Announcement.DateStart.Time) || now.After(setting.Announcement.DateEnd.Time) {
		logger.Info("Announcement is not in the effective date range")
		return
	}
	lang := i18n.NewLang(h.i18nLang)
	announcement := Announcement{
		GreenBoldColor: "\033[1;32m",
		ColorEnd:       "\033[0m",
		Title:          utils.CharNewLine + lang.T("Announcement: ") + setting.Announcement.Subject + utils.CharNewLine,
		Content:        PrettyContent(setting.Announcement.Content) + utils.CharNewLine,
	}
	tmpl := template.Must(template.New("announcement").Parse(announcementTmpl))
	if err := tmpl.Execute(sess, announcement); err != nil {
		logger.Error(err)
	}
	utils.IgnoreErrWriteString(sess, utils.CharNewLine)
}

type Announcement struct {
	GreenBoldColor string
	ColorEnd       string
	Title          string
	Content        string
}

var announcementTmpl = `{{.GreenBoldColor}}{{.Title }}{{.ColorEnd}}
{{.GreenBoldColor}}{{.Content}}{{.ColorEnd}}`

func PrettyContent(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\n\n", "\n")
	return strings.ReplaceAll(s, "\n", "\r\n")
}
