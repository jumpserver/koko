package proxy

import (
	"bytes"
	"regexp"
	"strings"

	"cocogo/pkg/utils"
)

var ps1Pattern = regexp.MustCompile(`^\[?.*@.*\]?[\\$#]\s|mysql>\s`)

func NewCmdParser() *CmdParser {
	parser := &CmdParser{}
	parser.initial()
	return parser
}

type CmdParser struct {
	term *utils.Terminal
	buf  *bytes.Buffer
}

func (cp *CmdParser) Reset() {
	cp.buf.Reset()
}

func (cp *CmdParser) initial() {
	cp.buf = new(bytes.Buffer)
	cp.term = utils.NewTerminal(cp.buf, "")
	cp.term.SetEcho(false)
}

func (cp *CmdParser) parsePS1(s string) string {
	return ps1Pattern.ReplaceAllString(s, "")
}

func (cp *CmdParser) Parse(b []byte) string {
	cp.buf.Write(b)
	cp.buf.WriteString("\r")
	lines, _ := cp.term.ReadLines()
	cp.Reset()
	output := strings.TrimSpace(strings.Join(lines, "\r\n"))
	output = cp.parsePS1(output)
	return output
}
