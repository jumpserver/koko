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

func (h *InteractiveHandler) displayBanner(sess io.ReadWriter, user string, termConf *model.TerminalConfig) {
	lang := i18n.NewLang(h.i18nLang)
	defaultTitle := lang.T("Welcome to use JumpServer open source fortress system")
	menu := Menu{
		{instruct: lang.T("/ + IP, Hostname, Comment"), helpText: lang.T("Enter {key} to search, such as: /192.168")},
		{instruct: "p", helpText: lang.T("Enter {key} to view all assets")},
		{instruct: "g", helpText: lang.T("Enter {key} to view the authorization tree")},
		{instruct: "c", helpText: lang.T("Enter {key} to view the type tree")},
		{instruct: "f", helpText: lang.T("Enter {key} to view the favorites tree")},
		{instruct: "s", helpText: lang.T("Enter {key} to change the interface language")},
		{instruct: "t", helpText: lang.T("Enter {key} to switch to TUI mode")},
		{instruct: "?", helpText: lang.T("Enter {key} to view help")},
		{instruct: "q", helpText: lang.T("Enter {key} to end this session")},
	}

	title := defaultTitle
	if termConf.HeaderTitle != "" {
		title = termConf.HeaderTitle
	}
	width, _ := h.GetPtySize()
	if width < 60 {
		utils.IgnoreErrWriteString(sess, utils.CharClear)
		for _, line := range classicHintLines(user+",", width) {
			utils.IgnoreErrWriteString(sess, utils.WrapperTitle(line)+utils.CharNewLine)
		}
		for _, line := range classicHintLines(title, width) {
			if termConf.HeaderTitle == "" {
				line = utils.WrapperTitle(line)
			}
			utils.IgnoreErrWriteString(sess, line+utils.CharNewLine)
		}
		utils.IgnoreErrWriteString(sess, utils.CharNewLine)
		for i, item := range menu {
			prefix := fmt.Sprintf(" %d) ", i+1)
			message := strings.ReplaceAll(item.helpText, "{key}", item.instruct)
			searchKeyHighlighted := false
			for _, line := range classicIndentedLines(prefix, message, width) {
				if i == 0 {
					if !searchKeyHighlighted && strings.Contains(line, "/") {
						line = strings.Replace(line, "/", utils.WrapperTitle("/"), 1)
						searchKeyHighlighted = true
					}
				} else {
					line = strings.Replace(line, " "+item.instruct, " "+utils.WrapperTitle(item.instruct), 1)
				}
				utils.IgnoreErrWriteString(sess, line+utils.CharNewLine)
			}
		}
		return
	}

	prefix := utils.CharClear + utils.CharTab + utils.CharTab
	suffix := utils.CharNewLine + utils.CharNewLine
	styledTitle := title
	if termConf.HeaderTitle == "" {
		styledTitle = utils.WrapperTitle(title)
	}
	welcomeMsg := prefix + utils.WrapperTitle(user+",") + "  " + styledTitle + suffix
	if _, err := io.WriteString(sess, welcomeMsg); err != nil {
		logger.Errorf("Send to client error, %s", err)
		return
	}
	for i, item := range menu {
		key := utils.WrapperTitle(item.instruct)
		if i == 0 {
			key = utils.WrapperTitle("/") + strings.TrimPrefix(item.instruct, "/")
		}
		line := strings.ReplaceAll(item.helpText, "{key}", key)
		if _, err := fmt.Fprintf(sess, "\t%2d) %s\r\n", i+1, line); err != nil {
			logger.Error(err)
		}
	}
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
	width, _ := h.GetPtySize()
	if width < 60 {
		message := lang.T("Announcement: ") + setting.Announcement.Subject + "\n" + setting.Announcement.Content
		for _, section := range strings.Split(PrettyContent(message), utils.CharNewLine) {
			for _, line := range classicHintLines(section, width) {
				utils.IgnoreErrWriteString(sess, utils.WrapperTitle(line)+utils.CharNewLine)
			}
		}
		utils.IgnoreErrWriteString(sess, utils.CharNewLine)
		return
	}
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
