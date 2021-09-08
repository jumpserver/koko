package auth

import (
	"strings"
	"text/template"
)

var (
	confirmInstruction = "Please wait for your admin to confirm."
	confirmQuestion    = "Do you want to continue [Y/n]? : "
)

const (
	actionAccepted        = "Accepted"
	actionFailed          = "Failed"
	actionPartialAccepted = "Partial accepted"
)

func CreateSelectOptionsQuestion(options []string) string{
	opts := make([]mfaOption, 0, len(options))
	for i := range options {
		opts = append(opts, mfaOption{
			Index: i + 1,
			Value: strings.ToUpper(options[i]),
		})
	}
	var out strings.Builder
	_ = mfaSelectTmpl.Execute(&out, opts)
	return out.String()
}

var (
	mfaSelectTmpl        = template.Must(template.New("mfaOptions").Parse(mfaOptions))
	mfaSelectInstruction = "Please Select MFA Option"
)

type mfaOption struct {
	Index int
	Value string
}

var mfaOptions = `{{ range . }}{{ .Index }}. {{.Value}}
{{end}}Option> `

var (
	mfaOptionInstruction = "Please Enter MFA Code."
	mfaOptionQuestion    = "[%s Code]: "
)
