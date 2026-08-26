package proxy

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"os"
	"regexp"
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
	Terminal *terminalparser.TerminalVT
	state    int
	once     sync.Once
	mux      sync.Mutex

	IsEnter func(p []byte) bool
	cmd     string

	commands []string

	EmitCommands func(cmd, out string)

	srvOutputBuf bytes.Buffer
	width        uint16
	height       uint16
}

func (s *TerminalParser) SetState(state int) {
	s.state = state
}

func (s *TerminalParser) resetCommand() {
	s.cmd = ""
	s.commands = nil
}

func (s *TerminalParser) GetCursorRow() string {
	row, err := s.Terminal.CursorRow()
	if err != nil {
		return ""
	}
	return row
}

func (s *TerminalParser) feed(p []byte) {
	s.Terminal.Write(p)
	if terminalDebug {
		fmt.Println("---------Feed-------------")
		fmt.Println(hex.Dump(p))
		fmt.Println("current row: ", s.GetCursorRow())
		fmt.Println()
	}
}

func (s *TerminalParser) Feed(p []byte) {
	s.mux.Lock()
	defer s.mux.Unlock()

	s.feed(p)
	s.processOutput(p)
}

// FeedScreen only updates VT state. It is used by full-screen editors so
// alternate-screen transitions remain observable without parsing keystrokes
// or recording editor repaint data as command output.
func (s *TerminalParser) FeedScreen(p []byte) {
	s.mux.Lock()
	defer s.mux.Unlock()
	s.feed(p)
}

// ProcessOutput parses command completion after the same bytes have already
// been applied through FeedScreen.
func (s *TerminalParser) ProcessOutput(p []byte) {
	s.mux.Lock()
	defer s.mux.Unlock()
	s.processOutput(p)
}

func (s *TerminalParser) processOutput(p []byte) {
	if s.state == OutputState {
		// output 且解析出 cmd 才写入 output 减少内存
		if s.srvOutputBuf.Len() < maxBufSize {
			s.srvOutputBuf.Write(p)
		} else {
			// 长时间输出达到最大值，直接命令结算一次
			outputBuf := s.trySrvOutput()
			if s.EmitCommands != nil {
				s.EmitCommands(s.cmd, outputBuf)
				s.cmd = ""
				return
			}
		}
		ps1 := s.Ps1sStr
		half := len(ps1) / 2
		halfPs1 := ps1[:half]
		rowStr := s.GetCursorRow()
		// 单行的命令解析
		if strings.HasPrefix(rowStr, halfPs1) && s.cmd != "" {
			outputBuf := s.trySrvOutput()
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

func (s *TerminalParser) IsScreenAlternate() bool {
	s.mux.Lock()
	defer s.mux.Unlock()
	alternate, err := s.Terminal.IsScreenAlternate()
	return err == nil && alternate
}

func (s *TerminalParser) Resize(width, height int) error {
	if width <= 0 || height <= 0 || width > 65535 || height > 65535 {
		return terminalparser.ErrInvalidSize
	}
	s.mux.Lock()
	defer s.mux.Unlock()
	if err := s.Terminal.Resize(uint16(width), uint16(height), 0, 0); err != nil {
		return err
	}
	s.width = uint16(width)
	s.height = uint16(height)
	return nil
}

func (s *TerminalParser) Close() error {
	return s.Terminal.Close()
}

func (s *TerminalParser) parseSrvOutputRows() []string {
	output := s.srvOutputBuf.Bytes()
	output = tmuxBar2Regx.ReplaceAll(output, []byte{})
	options := []terminalparser.Option{terminalparser.WithMaxScrollback(500)}
	if s.width > 0 && s.height > 0 {
		options = append(options, terminalparser.WithSize(s.width, s.height))
	}
	outputs, _ := terminalparser.Parse(output, options...)
	return outputs
}

func (s *TerminalParser) resetSrvOutput() {
	s.srvOutputBuf.Reset()
	if s.srvOutputBuf.Cap() > maxBufSize {
		s.srvOutputBuf = bytes.Buffer{}
	}
}

func (s *TerminalParser) trySrvOutput() string {
	outputs := s.parseSrvOutputRows()
	var str strings.Builder
	ps1 := strings.TrimSpace(s.Ps1sStr)
	for _, o := range outputs {
		o = strings.TrimSpace(o)
		o = strings.ReplaceAll(o, ps1, "")
		if len(o) > 0 && str.Len() < maxBufSize {
			str.WriteString(o)
			str.WriteString("\n")
		}
	}
	s.resetSrvOutput()
	return str.String()
}

func (s *TerminalParser) TryOutput() string {
	s.mux.Lock()
	defer s.mux.Unlock()
	s.cmd = ""
	return s.trySrvOutput()
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
	s.tryMultipleCommands()

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
			if s.cmd == "" && cmd != "" && len(s.commands) == 0 {
				if IsPasswordPrompt(s.Ps1sStr) {
					if terminalDebug {
						fmt.Println("============ password Input ignore =============")
						fmt.Println("ps1: ", s.Ps1sStr)
						fmt.Println("inputStr:", inputStr)
					}
					cmd = ""
					s.cmd = cmd
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

func (s *TerminalParser) findCommands(rows, cmds []string, startCmd string) {
	// 从最后一行开始往前查询命令
	outputs := make([]string, 0, 10)
	j := len(rows) - 1

	// 去除 startCMd的干扰
	for j >= 0 {
		row := rows[j]
		j--
		if strings.Contains(row, startCmd) {
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
		for j >= 0 {
			rowStr := rows[j]
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

func (s *TerminalParser) tryMultipleCommands() {
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
		rows := s.parseSrvOutputRows()
		s.findCommands(rows, commands, startCommand)
		s.resetSrvOutput()
		s.commands = nil
	}
}

func (s *TerminalParser) TryMultipleCommands() {
	s.mux.Lock()
	defer s.mux.Unlock()
	s.tryMultipleCommands()
}

func reverseString(rows []string) string {
	var str strings.Builder

	for i := len(rows) - 1; i >= 0; i-- {
		str.WriteString(rows[i])
		str.Write([]byte{'\r', '\n'})
	}
	return str.String()
}

// filtering for password input scenarios
var passwordPromptRegexps = []*regexp.Regexp{
	regexp.MustCompile(`(?i)password:?$`),                    // 常见的 Password:
	regexp.MustCompile(`(?i)\[sudo]\s*password\s*for\s+.*:`), // [sudo] password for user:
	regexp.MustCompile(`(?i)enter\s+passphrase\s+for\s+.*:`), // SSH/GPG 私钥 passphrase
	regexp.MustCompile(`(?i)passphrase\s+for\s+key\s+.*:`),   // git/ssh key 提示
	regexp.MustCompile(`(?i)请输入密码[:：]?$`),
	regexp.MustCompile(`(?i)mot de passe[:：]?$`),
	regexp.MustCompile(`(?i)contraseña[:：]?$`),
	regexp.MustCompile(`(?i)senha[:：]?$`),
}

func IsPasswordPrompt(ps1 string) bool {
	ps1 = strings.TrimSpace(ps1)
	for _, re := range passwordPromptRegexps {
		if re.MatchString(ps1) {
			return true
		}
	}
	return false
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
