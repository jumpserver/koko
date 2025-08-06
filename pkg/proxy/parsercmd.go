package proxy

import (
	"bytes"
	"fmt"
	"os"
	"runtime/debug"
	"strings"
	"sync"
	"unicode"

	"github.com/LeeEirc/terminalparser"
)

var terminalDebug = false

func init() {
	if os.Getenv("TERMINALPARSER") != "" {
		terminalDebug = true
	}
}

func DefaultEnterKeyPressHandler(p []byte) bool {
	return bytes.ContainsRune(p, '\r')
}

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

	commands []string

	EmitCommands func(cmd, out string)
}

func (s *TerminalParser) SetState(state int) {
	s.state = state
}

func (s *TerminalParser) resetCommand() {
	s.cmd = ""
	s.commands = nil
}

func (s *TerminalParser) Feed(p []byte) {
	defer func() {
		if r := recover(); r != nil {
			if terminalDebug {
				fmt.Printf("Recovered from panic: %s %s\n", r, string(debug.Stack()))
			}
		}
	}()
	s.mux.Lock()
	defer s.mux.Unlock()
	s.Screen.Feed(p)
	if s.state == OutputState {
		currentRow := s.Screen.GetCursorRow()
		// 单行的命令解析
		if currentRow.String() == s.Ps1sStr && s.cmd != "" {
			outputBuf := s.TryOutput()
			if s.EmitCommands != nil {
				s.EmitCommands(s.cmd, outputBuf)
			}
			if terminalDebug {
				// 从这里找上一个匹配的 ps1 row，然后这之间的 rows 就是output
				fmt.Println("============= match ps1 command================")
				fmt.Println("ps1: ", s.Ps1sStr)
				fmt.Println("command input:  ", s.cmd)
				fmt.Println("command output: ", outputBuf)
				fmt.Println("===============================================")
				// 这个时候应该是 输入状态了，命令结束了
			}
			s.cmd = ""
			return
		}

		// 多行命令 解析需要等完整输出，等下次输入的结果中，解析数据。参见WriteInput 里对 len(s.commands) >= 1  的处理
	}
	s.PrintLatestLines(10)
}

func (s *TerminalParser) OnSize() {

}

func (s *TerminalParser) PrintLatestLines(num int) {
	if !terminalDebug {
		return
	}
	maxRow := len(s.Screen.Rows)
	start := maxRow - num
	if start < 0 {
		start = 0
	}
	for i := start; i < maxRow; i++ {
		fmt.Println(s.Screen.Rows[i].String())
	}
}

func (s *TerminalParser) TryOutput() string {
	// 从这里找上一个匹配的 ps1 row，然后这之间的 rows 就是output
	rows := s.Screen.Rows
	maxRows := len(rows) - 1
	outputRows := make([]string, 0, maxRows)
	var matchCmd bool
	ps1 := s.Ps1sStr
	half := len(ps1) / 2
	halfPs1 := ps1[:half]
	for i := maxRows - 1; i >= 0; i-- {
		row := rows[i]
		rowStr := row.String()
		// insert row to outputRows first
		if strings.HasPrefix(rowStr, s.Ps1sStr) && strings.HasPrefix(rowStr, halfPs1) {
			matchCmd = true
			break
		}
		outputRows = append(outputRows, rowStr)
	}
	if !matchCmd {
		return ""
	}
	s.ResizeRows()
	return reverseString(outputRows)
}

func (s *TerminalParser) ResizeRows() {
	rowsLen := len(s.Screen.Rows)
	oldRows := s.Screen.Rows
	if rowsLen > 5000 {
		rows := make([]*terminalparser.Row, 0, 3000)
		start := rowsLen - 3000
		rows = append(rows, oldRows[start:]...)
		s.Screen.Rows = rows
	}
}

func IsPrintable(s string) bool {
	for _, r := range s {
		switch r {
		case '\t', '\n', '\r':
			continue
		default:
		}
		if !unicode.IsPrint(r) {
			return false
		}
	}
	return true
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

	// 复制粘贴多行命令执行
	s.TryMultipleCommands()

	isEnterFunc := DefaultEnterKeyPressHandler
	if s.IsEnter != nil {
		isEnterFunc = s.IsEnter
	}

	/*
		如果是多行命令，先完全解析下 input 内容做拦截，具体的执行命令及结果，则从命令解析器里面查找内容
	*/
	s.InputBuf.Write(chars)
	if isEnterFunc(chars) {
		// 针对多行命令，从最新一行，往前查找到最近一次的 ps1 之间的都是命令
		inputStr := strings.TrimSpace(s.InputBuf.String())
		s.state = OutputState
		cmd := s.TryInput()

		if cmd == "" && len(chars) > 1 {
			//从返回值解析，cmd 为 空的情况下，当前输入的则为
			cmd = strings.TrimSpace(string(chars))
			if strings.Contains(cmd, "\r") {
				// 多行命令
				s.commands = strings.Split(cmd, "\r")
			}
		} else {
			s.cmd = cmd
			suffixCmd := cmd[len(cmd)/2:]
			if IsPrintable(inputStr) {
				if strings.Contains(inputStr, suffixCmd) {
					cmd = inputStr
				} else if strings.Contains(inputStr, "\r") {
					s.commands = strings.Split(inputStr, "\r")
					cmd = inputStr
				}
			}
		}
		if terminalDebug {
			// 从这里找上一个匹配的 ps1 row，然后这之间的 rows 就是output
			fmt.Println("============= enter command================")
			fmt.Println("ps1: ", s.Ps1sStr)
			fmt.Println("command input1:  ", cmd)
			fmt.Println("command input2:  ", s.cmd)
			fmt.Println("commands :  ", s.commands)
			fmt.Println("===============================================")
			// 这个时候应该是 输出状态了，命令结束了
		}
		return cmd, true
	}
	if s.state == OutputState {
		s.state = InputState
		s.Ps1sStr = s.GetPs1()
	}
	return "", false
}

func (s *TerminalParser) TryInput() string {
	lastLine := s.Screen.GetCursorRow()
	cmd := strings.TrimPrefix(lastLine.String(), s.Ps1sStr)
	s.InputBuf.Reset()
	return strings.TrimSpace(cmd)
}

func (s *TerminalParser) GetPs1() string {
	row := s.Screen.GetCursorRow()
	rowStr := row.String()
	return strings.TrimSuffix(rowStr, s.InputBuf.String())
}

func (s *TerminalParser) FindCommands(cmds []string, startCmd string) {
	// 从最后一行开始往前查询命令
	outputs := make([]string, 0, 10)
	rows := s.Screen.Rows
	j := len(rows) - 1

	// 去除 startCMd的干扰
	for j > 0 {
		row := rows[j]
		j--
		if strings.Contains(row.String(), startCmd) {
			break
		}
	}
	ps1 := s.Ps1sStr
	half := len(ps1) / 2
	halfPs1 := ps1[:half]
	if terminalDebug {
		fmt.Println("ps1: ", ps1, " halfPs1: ", halfPs1)
	}
	for i := len(cmds) - 1; i >= 0; i-- {
		currentCommand := cmds[i]
		if currentCommand == "" {
			continue
		}
		for j > 0 {
			row := rows[j]
			rowStr := row.String()
			j--
			if strings.Contains(rowStr, currentCommand) && strings.Contains(rowStr, halfPs1) {
				// 匹配到 当前的命令，获取下所有的output
				output := reverseString(outputs)
				if s.EmitCommands != nil {
					s.EmitCommands(currentCommand, output)
					if terminalDebug {
						fmt.Println("-----------EmitCommands----------- ")
						fmt.Println("command input:  ", currentCommand)
						fmt.Println("command output: ", output)
					}
				}
				outputs = make([]string, 0, 10)
				break
			}
			outputStr := strings.TrimPrefix(rowStr, s.Ps1sStr)
			if outputStr != "" {
				outputs = append(outputs, outputStr)
			}
		}
	}
}

func (s *TerminalParser) CurrentRowHasPs1() bool {
	row := s.Screen.GetCursorRow()
	rowStr := row.String()
	return strings.Contains(rowStr, s.Ps1sStr)
}

func (s *TerminalParser) TryMultipleCommands() {
	if len(s.commands) >= 1 {
		commands := s.commands

		// 需要从返回的数据里，获取到当前的命令结果
		lastCommand := commands[len(commands)-1]
		startCommand := lastCommand
		if startCommand == "" {
			startCommand = s.Ps1sStr
		} else {
			//排除最后一个未执行的
			commands = commands[:len(commands)-1]
		}
		if terminalDebug {
			for i := len(commands) - 1; i >= 0; i-- {
				cmd := commands[i]
				fmt.Printf("may be command: `%s`\n", cmd)
			}
		}
		s.FindCommands(commands, startCommand)
		s.commands = nil
	}
}

func reverseString(rows []string) string {
	var str strings.Builder

	for i := len(rows) - 1; i >= 0; i-- {
		str.WriteString(rows[i])
		str.Write([]byte{'\r', '\n'})
	}
	return str.String()
}
