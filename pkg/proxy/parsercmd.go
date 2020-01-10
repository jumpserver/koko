package proxy

import (
	"bytes"
	"regexp"
	"strings"
	"sync"

	"github.com/jumpserver/koko/pkg/utils"
)

var ps1Pattern = regexp.MustCompile(`^\[?.*@.*\]?[\\$#]\s|mysql>\s`)

func NewCmdParser() *CmdParser {
	parser := CmdParser{}
	parser.initial()
	return &parser
}

type CmdParser struct {
	id   string
	name string
	buf  bytes.Buffer

	lock          *sync.Mutex
	maxLength     int
	currentLength int
}

func (cp *CmdParser) WriteData(p []byte) (int, error) {
	cp.lock.Lock()
	defer cp.lock.Unlock()
	if cp.buf.Len() >= 1024 {
		return 0, nil
	}
	return cp.buf.Write(p)
}

func (cp *CmdParser) Close() error {
	return nil
}

func (cp *CmdParser) initial() {
	cp.lock = new(sync.Mutex)
}

func (cp *CmdParser) parsePS1(s string) string {
	return ps1Pattern.ReplaceAllString(s, "")
}

// Parse 解析命令或输出
func (cp *CmdParser) Parse() string {
	cp.lock.Lock()
	defer cp.lock.Unlock()
	lines := utils.ParseTerminalData(cp.buf.Bytes())
	output := strings.TrimSpace(strings.Join(lines, "\r\n"))
	output = cp.parsePS1(output)
	cp.buf.Reset()
	return output
}
