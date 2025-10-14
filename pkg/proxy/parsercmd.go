package proxy

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"os"
	"regexp"
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

const (
	LinuxScreen = iota + 1
	UsqlScreen
	MongoScreen
	TmuxScreen
	WindowsScreen
)

func DefaultEnterKeyPressHandler(p []byte) bool {
	return bytes.ContainsRune(p, '\r')
}

const maxBufSize = 1024 * 100

const maxOutPutBuffer = 1024 * 512

const (
	InputPreState = iota + 1
	InputState
	InVimState
	OutputState
)

type ScreenParser interface {
	Feed([]byte)
	GetCursorRow() string
}

type TerminalParser struct {
	InputBuf bytes.Buffer
	Ps1sStr  string
	Screen   *terminalparser.Screen
	state    int
	once     sync.Once
	mux      sync.Mutex

	IsEnter func(p []byte) bool
	cmd     string

	commands []string

	EmitCommands func(cmd, out string)

	tmuxParser *terminalparser.TmuxParser
	isSubMode  bool

	srvOutputBuf bytes.Buffer

	screenType    int
	preScreenType int
	//screenParser ScreenParser

	winScreenParser   *terminalparser.WindowsParser
	mongoScreenParser *terminalparser.MongoShParser
	usqlScreenParser  *terminalparser.USqlParser
}

func (s *TerminalParser) SetState(state int) {
	s.state = state
}

func (s *TerminalParser) CheckSubScreen(b []byte) {
	if !s.isSubMode && IsEditEnterMode(b) {
		s.isSubMode = true
		s.tmuxParser = terminalparser.NewTmuxParser()
		s.screenType = TmuxScreen
	}
	if s.isSubMode && IsEditExitMode(b) {
		s.isSubMode = false
		s.tmuxParser = nil
		s.srvOutputBuf.Reset()
		s.screenType = s.preScreenType
	}
}

func (s *TerminalParser) resetCommand() {
	s.cmd = ""
	s.commands = nil
}

func (s *TerminalParser) GetCursorRow() string {
	switch s.screenType {
	case LinuxScreen:
		row := s.Screen.GetCursorRow()
		return row.String()
	case UsqlScreen:
		row := s.usqlScreenParser.TmuxScreen.GetCursorRow()
		return row.String()
	case MongoScreen:
		row := s.mongoScreenParser.TmuxScreen.GetCursorRow()
		return row.String()
	case TmuxScreen:
		row := s.tmuxParser.TmuxScreen.GetCursorRow()
		return row.String()
	default:
		row := s.Screen.GetCursorRow()
		return row.String()
	}
}

func (s *TerminalParser) feed(p []byte) {
	defer func() {
		if r := recover(); r != nil {
			if terminalDebug {
				fmt.Printf("Recovered from panic: %s %s\n", r, string(debug.Stack()))
			}
		}
	}()

	switch s.screenType {
	case UsqlScreen:
		s.usqlScreenParser.Feed(p)
	case MongoScreen:
		s.mongoScreenParser.Feed(p)
	case TmuxScreen:
		s.tmuxParser.Feed(p)
	//case LinuxScreen:
	//	s.Screen.Feed(p)
	//	s.ResizeRows()
	default:
		// 默认就是 LinuxScreen
		s.Screen.Feed(p)
		s.ResizeRows()
	}
	if terminalDebug {
		fmt.Println("---------Feed-------------")
		fmt.Println(hex.Dump(p))
		fmt.Println("current row: ", s.GetCursorRow())
		fmt.Println()
	}
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
	// 检测是否是 tmux 和 screen 的情况
	s.CheckSubScreen(p)

	s.feed(p)

	if s.state == OutputState {
		outputSize := len(s.srvOutputBuf.Bytes())
		if outputSize < maxOutPutBuffer {
			s.srvOutputBuf.Write(p)
		}
		ps1 := s.Ps1sStr
		half := len(ps1) / 2
		halfPs1 := ps1[:half]
		rowStr := s.GetCursorRow()
		// 单行的命令解析
		if strings.HasPrefix(rowStr, halfPs1) && s.cmd != "" {
			outputBuf := s.TrySrvOutput()
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
}

func (s *TerminalParser) OnSize(w, h int) {

}

func (s *TerminalParser) TrySrvOutput() string {
	output := s.srvOutputBuf.Bytes()
	output = tmuxBar2Regx.ReplaceAll(output, []byte{})
	outputs := TryParseResult(output)
	var str strings.Builder
	ps1 := strings.TrimSpace(s.Ps1sStr)
	for i := range outputs {
		o := outputs[i]
		o = strings.TrimSpace(o)
		o = strings.ReplaceAll(o, ps1, "")
		o = strings.TrimSpace(o)
		if len(o) > 0 {
			str.WriteString(o)
			str.WriteString("\n")
		}
	}
	s.srvOutputBuf.Reset()
	return str.String()
}

func (s *TerminalParser) TryOutput() string {
	return s.TrySrvOutput()
}

func (s *TerminalParser) ResizeRows() {
	//rowsLen := len(s.Screen.Rows)
	//if rowsLen > maxRows {
	//	s.Screen.Rows = trimRows(s.Screen.Rows)
	//	s.Screen.Cursor.Y = keepRows
	//}
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
		inputStr := strings.TrimSpace(s.InputBuf.String())
		s.state = OutputState
		//if s.isSubMode {
		//	cmd = s.TryTmuxInput()
		//} else {
		//	// 针对多行命令，从最新一行，往前查找到最近一次的 ps1 之间的都是命令
		//	cmd = s.TryInput()
		//}
		cmd := s.TryLastRowInput()
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
		if s.cmd != "" {
			outputBuf := s.TrySrvOutput()
			s.EmitCommands(s.cmd, outputBuf)
			s.cmd = ""
		}
		s.state = InputState
		s.Ps1sStr = s.GetPs1()
	}
	return "", false
}

func (s *TerminalParser) TryTmuxInput() string {
	lastLine := s.tmuxParser.TmuxScreen.GetCursorRow()
	cmd := strings.TrimPrefix(lastLine.String(), s.Ps1sStr)
	s.InputBuf.Reset()
	return strings.TrimSpace(cmd)
}

func (s *TerminalParser) TryInput() string {
	lastLine := s.Screen.GetCursorRow()
	cmd := strings.TrimPrefix(lastLine.String(), s.Ps1sStr)
	s.InputBuf.Reset()
	return strings.TrimSpace(cmd)
}

func (s *TerminalParser) TryLastRowInput() string {
	rowStr := s.GetCursorRow()
	cmd := strings.TrimPrefix(rowStr, s.Ps1sStr)
	s.InputBuf.Reset()
	return strings.TrimSpace(cmd)
}

func (s *TerminalParser) GetPs1() string {
	rowStr := s.GetCursorRow()
	return strings.TrimSuffix(rowStr, s.InputBuf.String())
}

func (s *TerminalParser) FindCommands(cmds []string, startCmd string) {
	// 从最后一行开始往前查询命令
	outputs := make([]string, 0, 10)
	rows := s.Screen.Rows.Values()
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
	if s.screenType != LinuxScreen {
		// 仅 linux screen方式支持
		return
	}
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

func TryParseResult(p []byte) []string {
	defer func() {
		if r := recover(); r != nil {
			if terminalDebug {
				fmt.Println("Recovered in TryParseResult", r, string(debug.Stack()))
			}
		}
	}()
	return terminalparser.ParseOutput(p)
}

// 合并的正则表达式，匹配以下四种模式：
// 1. 隐藏光标: ESC[?25l
// 2. ANSI颜色转义序列: ESC[数字m
// 3. ANSI位置转义序列: ESC[数字;数字H
// 4. 数字开头的状态栏格式: [数字] 空格 内容 空格 内容...
// 0D 0A \r \n
var (
	tmuxBarRegx = regexp.MustCompile(`\x1b\[\?(\d+)l\x1b\[(\d+)m\x1b\[(\d+)m\x1b\[(\d+);(\d+)H\[(\d+)]\s+\d+:.+\s+.+\s+.+\s+.+\x1b\(B.*\x1b\[\?(\d+)l\x1b\[\?(\d+)h`)
	// \[(\d+)]\s+\d+:.+\s+.+\s+.+\s+.+

	// 可能包含 \r\n
	//tmuxBar1Regx = regexp.MustCompile(`\r\n\[(\d+)]\s+\d+:.+\s+.+\s+.+\s+.+\x1b\(B`)

	// 不包含 \r\n
	tmuxBar2Regx = regexp.MustCompile(`\[(\d+)]\s+\d+:.+\s+.+\s+.+\s+.+\x1b\(B`)
)
