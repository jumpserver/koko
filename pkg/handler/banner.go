package handler

import (
	"bytes"
	"fmt"
	"io"
	"sync"
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
	mi.showText = buf.String()
	return mi.showText
}

type Menu []MenuItem

func Initial() {
	defaultTitle = utils.WrapperTitle(i18n.T("Welcome to use Jumpserver open source fortress system"))
	menu = Menu{
		{id: 1, instruct: i18n.T("part IP, Hostname, Comment"), helpText: i18n.T("to search login if unique")},
		{id: 2, instruct: i18n.T("/ + IP, Hostname, Comment"), helpText: i18n.T("to search, such as: /192.168")},
		{id: 3, instruct: "p", helpText: i18n.T("display the host you have permission")},
		{id: 4, instruct: "g", helpText: i18n.T("display the node that you have permission")},
		{id: 5, instruct: "r", helpText: i18n.T("refresh your assets and nodes")},
		{id: 6, instruct: "h", helpText: i18n.T("print help")},
		{id: 7, instruct: "q", helpText: i18n.T("exit")},
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

var i18nMap map[string]string
var i18nOnce sync.Once

func getI18nFromMap(name string) string {
	i18nOnce.Do(func() {
		i18nMap = map[string]string{
			"ID":                i18n.T("ID"),
			"Hostname":          i18n.T("hostname"),
			"IP":                i18n.T("IP"),
			"Comment":           i18n.T("comment"),
			"AssetTableCaption": i18n.T("Page: %d, Count: %d, Total Page: %d, Total Count: %d"),
			"NoAssets":          i18n.T("No Assets"),
			"LoginTip":          i18n.T("Enter ID number directly login the asset, multiple search use // + field, such as: //16"),
			"PageActionTip":     i18n.T("Page up: b	Page down: n"),
			"NodeHeaderTip":     i18n.T("Node: [ ID.Name(Asset amount) ]"),
			"NodeEndTip":        i18n.T("Tips: Enter g+NodeID to display the host under the node, such as g1"),
			"RefreshDone":       i18n.T("Refresh done"),
			"SelectUserTip":     i18n.T("Tips: Enter system user ID and directly login the asset [ %s(%s) ]"),
			"BackTip":           i18n.T("Back: B/b"),
			"Name":              i18n.T("Name"),
			"Username":          i18n.T("Username"),
			"All":               i18n.T("all"),
			"SearchTip":         i18n.T("Search: %s"),
			"DBType":            i18n.T("DBType"),
		}
	})
	return i18nMap[name]
}
