package proxy

import (
	"fmt"
	"io"
	"regexp"
	"strings"
	"sync"

	"github.com/jumpserver/koko/pkg/logger"
	"github.com/jumpserver/koko/pkg/utils"
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
		logger.Infof("Session %s: %s start", cp.id, cp.name)
		defer logger.Infof("Session %s: %s parser close", cp.id, cp.name)
	loop:
		for {
			line, err := cp.term.ReadLine()
			if err != nil {
				select {
				case <-cp.closed:
					break loop
				default:
				}
				goto loop
			}
			cp.lock.Lock()
			cp.currentLength += len(line)
			if cp.currentLength < cp.maxLength {
				cp.currentLines = append(cp.currentLines, line)
			}
			cp.lock.Unlock()
		}
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

func NewCommandInputParser(sid, name string) *CommandInputParser {
	parser := CommandInputParser{id: sid, name: name}
	parser.initial()
	return &parser
}

type CommandInputParser struct {
	id   string
	name string

	term          *utils.Terminal
	reader        io.ReadCloser
	writer        io.WriteCloser
	currentLines  []string
	lock          sync.Mutex
	maxLength     int
	currentLength int
	closed        chan struct{}

	userInputData []byte
	srvBackData []byte
}

func (cp *CommandInputParser) WriteUserData(p []byte) {
	cp.userInputData = append(cp.userInputData, p...)
}

func (cp *CommandInputParser) WriteData(p []byte) (int, error) {
	cp.srvBackData = append(cp.srvBackData, p...)
	return cp.writer.Write(p)
}

func (cp *CommandInputParser) Write(p []byte) (int, error) {
	return len(p), nil
}

func (cp *CommandInputParser) Read(p []byte) (int, error) {
	return cp.reader.Read(p)
}

func (cp *CommandInputParser) Close() error {
	select {
	case <-cp.closed:
		return nil
	default:
		close(cp.closed)
	}
	return cp.writer.Close()
}

func (cp *CommandInputParser) initial() {
	cp.reader, cp.writer = io.Pipe()
	cp.currentLines = make([]string, 0)
	cp.maxLength = 1024
	cp.currentLength = 0
	cp.closed = make(chan struct{})

	cp.term = utils.NewTerminal(cp, "")
	cp.term.SetEcho(false)
	go func() {
		logger.Infof("Session %s: %s start", cp.id, cp.name)
		defer logger.Infof("Session %s: %s parser close", cp.id, cp.name)
	loop:
		for {
			line, err := cp.term.ReadLine()
			if err != nil {
				select {
				case <-cp.closed:
					break loop
				default:
				}
				goto loop
			}
			cp.lock.Lock()
			cp.currentLength += len(line)
			if cp.currentLength < cp.maxLength {
				cp.currentLines = append(cp.currentLines, line)
			}
			cp.lock.Unlock()
		}
	}()
}

func (cp *CommandInputParser) parsePS1(s string) string {
	return ps1Pattern.ReplaceAllString(s, "")
}

// Parse 解析命令或输出
func (cp *CommandInputParser) Parse() string {
	cp.writer.Write([]byte("\r"))
	cp.lock.Lock()
	defer cp.lock.Unlock()
	output := strings.TrimSpace(strings.Join(cp.currentLines, "\r\n"))

	fmt.Println("out Parse:==> ", output)
	output = cp.parsePS1(output)
	cp.currentLines = make([]string, 0)
	cp.currentLength = 0
	return output
}

func (cp *CommandInputParser) ParseUserInput() string {
	lines := utils.ParseTerminalData(cp.userInputData)
	output := strings.TrimSpace(strings.Join(lines, "\r\n"))
	fmt.Println("ParseUserInput: ", output)
	cp.userInputData = cp.userInputData[:0]
	return output
}
