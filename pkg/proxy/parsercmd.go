package proxy

import (
	"bytes"
	"fmt"
	"strings"
	"sync"

	"github.com/LeeEirc/terminalparser"
)

const maxBufSize = 1024 * 100

const (
	InputPreState = iota + 1
	InputState
	InVimState
	OutputState
)

type TerminalParser struct {
	InputBuf bytes.Buffer
	Ps1sStr  string
	Screen   terminalparser.Screen
	state    int
	once     sync.Once
	mux      sync.Mutex

	IsEnter func(p []byte) bool
	cmd     string

	EmitCommands func(cmd, out string)
}

func (s *TerminalParser) Feed(p []byte) {
	defer func() {
		if r := recover(); r != nil {
			fmt.Println("Recovered from panic:", r)
		}
	}()
	s.mux.Lock()
	defer s.mux.Unlock()
	s.Screen.Feed(p)
	if s.state == OutputState {
		currentRow := s.Screen.GetCursorRow()
		if currentRow.String() == s.Ps1sStr && s.cmd != "" {
			// 从这里找上一个匹配的 ps1 row，然后这之间的 rows 就是output
			outputBuf := s.TryOutput()
			//fmt.Println("=============================")
			//fmt.Println("command input:  ", s.cmd)
			//fmt.Println("command output: ", outputBuf)
			if s.EmitCommands != nil {
				s.EmitCommands(s.cmd, outputBuf)
			}
			s.cmd = ""
			//fmt.Println("================")
			// 这个时候应该是 输入状态了，命令结束了
		}
	}

}

func (s *TerminalParser) OnSize() {

}

func (s *TerminalParser) TryOutput() string {
	// 从这里找上一个匹配的 ps1 row，然后这之间的 rows 就是output
	rows := s.Screen.Rows
	maxRows := len(rows) - 1
	outputRows := make([]string, 0, maxRows)
	for i := maxRows - 1; i >= 0; i-- {
		row := rows[i]
		// insert row to outputRows first
		if strings.HasPrefix(row.String(), s.Ps1sStr) {
			break
		}
		outputRows = append(outputRows, row.String())
	}
	var outputBuf bytes.Buffer
	for i := len(outputRows) - 1; i >= 0; i-- {
		outputBuf.WriteString(outputRows[i])
		outputBuf.Write([]byte{'\r', '\n'})
	}
	return outputBuf.String()
}

func DefaultEnterKeyPressHandler(p []byte) bool {
	return p[len(p)-1] == charEnter[0]
}

func (s *TerminalParser) WriteInput(chars []byte) (string, bool) {
	if len(chars) == 0 {
		return "", false
	}
	s.mux.Lock()
	defer s.mux.Unlock()
	s.once.Do(func() {
		s.state = InputState
		s.Ps1sStr = s.GetPs1()
	})
	isEnterFunc := DefaultEnterKeyPressHandler
	if s.IsEnter != nil {
		isEnterFunc = s.IsEnter
	}

	if isEnterFunc(chars) {
		s.state = OutputState
		lastLine := s.Screen.GetCursorRow()
		cmd := strings.TrimPrefix(lastLine.String(), s.Ps1sStr)
		s.InputBuf.Reset()
		s.cmd = cmd
		return cmd, true
	}
	if s.state == OutputState {
		s.state = InputState
		s.Ps1sStr = s.GetPs1()
	}
	s.InputBuf.Write(chars)
	return "", false
}

func (s *TerminalParser) GetPs1() string {
	row := s.Screen.GetCursorRow()
	rowStr := row.String()
	return strings.TrimSuffix(rowStr, s.InputBuf.String())
}
