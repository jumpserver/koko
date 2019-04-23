package handlers

import (
	"text/template"

	"github.com/gliderlabs/ssh"

	"cocogo/pkg/logger"
)

const bannerTemplate = `
		{{.UserName}}	Welcome to use Jumpserver open source fortress system{{.EndLine}}
{{.Tab}}1) Enter {{.ColorCode}}ID{{.ColorEnd}} directly login or enter {{.ColorCode}}part IP, Hostname, Comment{{.ColorEnd}} to search login(if unique). {{.EndLine}}
{{.Tab}}2) Enter {{.ColorCode}}/{{.ColorEnd}} + {{.ColorCode}}IP, Hostname{{.ColorEnd}} or {{.ColorCode}}Comment{{.ColorEnd}} search, such as: /ip. {{.EndLine}}
{{.Tab}}3) Enter {{.ColorCode}}p{{.ColorEnd}} to display the host you have permission.{{.EndLine}}
{{.Tab}}4) Enter {{.ColorCode}}g{{.ColorEnd}} to display the node that you have permission.{{.EndLine}}
{{.Tab}}5) Enter {{.ColorCode}}g{{.ColorEnd}} + {{.ColorCode}}NodeID{{.ColorEnd}} to display the host under the node, such as g1. {{.EndLine}}
{{.Tab}}6) Enter {{.ColorCode}}s{{.ColorEnd}} Chinese-english proxy.{{.EndLine}}
{{.Tab}}7) Enter {{.ColorCode}}h{{.ColorEnd}} help.{{.EndLine}}
{{.Tab}}8) Enter {{.ColorCode}}r{{.ColorEnd}} to refresh your assets and nodes.{{.EndLine}}
{{.Tab}}0) Enter {{.ColorCode}}q{{.ColorEnd}} exit.{{.EndLine}}
`

var displayTemplate = template.Must(template.New("display").Parse(bannerTemplate))

type Banner struct {
	UserName  string
	ColorCode string
	ColorEnd  string
	Tab       string
	EndLine   string
}

func (h *Banner) display(sess ssh.Session) {
	e := displayTemplate.Execute(sess, h)
	if e != nil {
		logger.Warn("Display help info failed")
	}
}

func NewBanner(userName string) *Banner {
	return &Banner{
		UserName:  userName,
		ColorCode: GreenColorCode,
		ColorEnd:  ColorEnd,
		Tab:       Tab,
		EndLine:   EndLine,
	}
}
