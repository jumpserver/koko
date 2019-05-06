package proxy

import (
	"bytes"
	"strings"

	"cocogo/pkg/utils"
)

type CmdParser struct {
	term *utils.Terminal
	buf  *bytes.Buffer
}

func (cp *CmdParser) Reset() {
	cp.buf.Reset()
}

func (cp *CmdParser) Initial() {
	cp.buf = new(bytes.Buffer)
	cp.term = utils.NewTerminal(cp.buf, "")
	cp.term.SetEcho(false)
}

func (cp *CmdParser) Parse(b []byte) string {
	cp.buf.Write(b)
	cp.buf.WriteString("\r")
	lines, _ := cp.term.ReadLines()
	return strings.TrimSpace(strings.Join(lines, "\r\n"))
}
