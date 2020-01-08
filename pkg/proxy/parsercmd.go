package proxy

import (
	"bytes"
	"github.com/jumpserver/koko/pkg/logger"
	"github.com/jumpserver/koko/pkg/utils"
	"regexp"
	"strings"
	"sync"
)

var ps1Pattern = regexp.MustCompile(`^\[?.*@.*\]?[\\$#]\s|mysql>\s`)

func NewCmdParser(sid, name string) *CmdParser {
	parser := CmdParser{id: sid, name: name}
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
	logger.Infof("session ID: %s, parser name: %s", cp.id, cp.name)
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
