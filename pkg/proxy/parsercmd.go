package proxy

import (
	"io"
	"regexp"
	"strings"
	"sync"

	"github.com/jumpserver/koko/pkg/logger"
	"github.com/jumpserver/koko/pkg/utils"
)

var ps1Pattern = regexp.MustCompile(`^\[?.*@.*\]?[\\$#]\s|mysql>\s`)

func NewCmdParser() *CmdParser {
	parser := &CmdParser{}
	parser.initial()
	return parser
}

type CmdParser struct {
	term          *utils.Terminal
	reader        io.ReadCloser
	writer        io.WriteCloser
	currentLines  []string
	lock          *sync.Mutex
	maxLength     int
	currentLength int
	closed        chan struct{}
}

func (cp *CmdParser) WriteData(p []byte) (int, error) {
	return cp.writer.Write(p)
}

func (cp *CmdParser) Write(p []byte) (int, error) {
	return len(p), nil
}

func (cp *CmdParser) Read(p []byte) (int, error) {
	return cp.reader.Read(p)
}

func (cp *CmdParser) Close() error {
	select {
	case <-cp.closed:
		return nil
	default:
		close(cp.closed)
	}
	return cp.writer.Close()
}

func (cp *CmdParser) initial() {
	cp.reader, cp.writer = io.Pipe()
	cp.currentLines = make([]string, 0)
	cp.lock = new(sync.Mutex)
	cp.maxLength = 1024
	cp.currentLength = 0
	cp.closed = make(chan struct{})

	cp.term = utils.NewTerminal(cp, "")
	cp.term.SetEcho(false)
	go func() {
		logger.Debug("command Parser start")
		defer logger.Debug("command Parser close")
	inloop:
		for {
			line, err := cp.term.ReadLine()
			if err != nil {
				select {
				case <-cp.closed:
					goto outloop
				default:
				}
				goto inloop
			}
			cp.lock.Lock()
			cp.currentLength += len(line)
			if cp.currentLength < cp.maxLength {
				cp.currentLines = append(cp.currentLines, line)
			}
			cp.lock.Unlock()
		}
	outloop:
	}()
}

func (cp *CmdParser) parsePS1(s string) string {
	return ps1Pattern.ReplaceAllString(s, "")
}

// Parse 解析命令或输出
func (cp *CmdParser) Parse() string {
	cp.writer.Write([]byte("\r"))
	cp.lock.Lock()
	defer cp.lock.Unlock()
	output := strings.TrimSpace(strings.Join(cp.currentLines, "\r\n"))
	output = cp.parsePS1(output)
	cp.currentLines = make([]string, 0)
	cp.currentLength = 0
	return output
}
