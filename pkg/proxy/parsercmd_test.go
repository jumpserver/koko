package proxy

import (
	"bytes"
	"strings"
	"testing"

	"github.com/LeeEirc/terminalparser"
	"github.com/jumpserver/koko/pkg/zmodem"
)

func TestTerminalParserCursorRow(t *testing.T) {
	terminal, err := terminalparser.New(terminalparser.WithSize(80, 24))
	if err != nil {
		t.Fatal(err)
	}
	parser := TerminalParser{Terminal: terminal, width: 80, height: 24}
	t.Cleanup(func() { _ = parser.Close() })

	parser.Feed([]byte("root@host:~$ echo hello"))
	if got := parser.GetCursorRow(); got != "root@host:~$ echo hello" {
		t.Fatalf("GetCursorRow() = %q", got)
	}

	parser.Feed([]byte("\r\nhello\r\nroot@host:~$ "))
	if got := parser.GetCursorRow(); got != "root@host:~$" {
		t.Fatalf("GetCursorRow() after output = %q", got)
	}
}

func TestTerminalParserCursorRowIgnoresTmuxStatusBar(t *testing.T) {
	terminal, err := terminalparser.New(
		terminalparser.WithSize(24, 4),
		terminalparser.WithMaxScrollback(0),
	)
	if err != nil {
		t.Fatal(err)
	}
	parser := TerminalParser{Terminal: terminal, width: 24, height: 4}
	t.Cleanup(func() { _ = parser.Close() })

	parser.Feed([]byte(
		"\x1b[?1049h" +
			"\x1b[1;1Huser@tmux$ echo hi" +
			"\x1b[4;1H[0] bash status" +
			"\x1b[1;19H",
	))
	if got := parser.GetCursorRow(); got != "user@tmux$ echo hi" {
		t.Fatalf("GetCursorRow() = %q", got)
	}
}

func TestTmuxAlternateScreenStillFeedsTerminalParser(t *testing.T) {
	terminal, err := terminalparser.New(
		terminalparser.WithSize(24, 4),
		terminalparser.WithMaxScrollback(0),
	)
	if err != nil {
		t.Fatal(err)
	}
	terminalParser := &TerminalParser{
		Terminal: terminal,
		state:    OutputState,
		width:    24,
		height:   4,
	}
	t.Cleanup(func() { _ = terminalParser.Close() })
	parser := Parser{
		TerminalParser: terminalParser,
		zmodemParser:   zmodem.New(),
		command:        "tmux attach-session",
	}

	parser.ParseServerOutput([]byte(
		"\x1b[?1049h" +
			"\x1b[1;1Huser@tmux$ echo hi" +
			"\x1b[2;1Htmux pane output" +
			"\x1b[4;1H[0] bash status" +
			"\x1b[1;19H",
	))
	if parser.inVimState {
		t.Fatal("tmux alternate screen was treated as vim")
	}
	if !parser.isScreenMode {
		t.Fatal("tmux alternate screen was not recognized as a multiplexer")
	}
	if got := terminalParser.GetCursorRow(); got != "user@tmux$ echo hi" {
		t.Fatalf("tmux data did not reach terminal parser: %q", got)
	}
	if terminalParser.srvOutputBuf.Len() == 0 {
		t.Fatal("tmux output was not retained for command parsing")
	}

	terminalParser.resetSrvOutput()
	parser.command = "echo still-parsed"
	parser.ParseServerOutput([]byte("\x1b[2;1Hcommand output"))
	if parser.inVimState || terminalParser.srvOutputBuf.Len() == 0 {
		t.Fatal("command parsing did not continue inside tmux")
	}
}

func TestVimUpdatesScreenWithoutParsingCommands(t *testing.T) {
	terminal, err := terminalparser.New(
		terminalparser.WithSize(40, 6),
		terminalparser.WithMaxScrollback(0),
	)
	if err != nil {
		t.Fatal(err)
	}
	terminalParser := &TerminalParser{
		Terminal: terminal,
		state:    OutputState,
		cmd:      "vim notes.txt",
		width:    40,
		height:   6,
	}
	t.Cleanup(func() { _ = terminalParser.Close() })
	parser := Parser{
		TerminalParser: terminalParser,
		zmodemParser:   zmodem.New(),
		command:        "vim notes.txt",
	}

	frame := []byte("\x1b[?1049h\x1b[HVim buffer\x1b[2;1H-- INSERT --")
	if forwarded := parser.ParseServerOutput(frame); !bytes.Equal(forwarded, frame) {
		t.Fatalf("vim frame changed in transit: %x", forwarded)
	}
	if parser.IsNeedParse() {
		t.Fatal("vim alternate screen did not suspend command parsing")
	}
	if got := terminalParser.GetCursorRow(); got != "-- INSERT --" {
		t.Fatalf("vim frame did not update TerminalVT: %q", got)
	}
	if terminalParser.srvOutputBuf.Len() != 0 {
		t.Fatalf("vim repaint entered command output buffer: %d", terminalParser.srvOutputBuf.Len())
	}

	parser.ParseUserInput([]byte("dd:w"))
	if terminalParser.InputBuf.Len() != 0 {
		t.Fatalf("vim keystrokes entered command input buffer: %q", terminalParser.InputBuf.String())
	}

	parser.ParseServerOutput([]byte("\x1b[?1049l"))
	if !parser.IsNeedParse() {
		t.Fatal("command parsing did not resume after leaving vim")
	}
}

func TestTerminalCommandKinds(t *testing.T) {
	tests := map[string]struct {
		editor      bool
		multiplexer bool
	}{
		"vim file":                      {editor: true},
		"sudo /usr/bin/nvim file":       {editor: true},
		"TERM=xterm-256color less file": {editor: true},
		"tmux attach-session":           {multiplexer: true},
		"sudo screen -x":                {multiplexer: true},
		"echo vim":                      {},
	}
	for command, expected := range tests {
		if got := isTerminalEditorCommand(command); got != expected.editor {
			t.Errorf("isTerminalEditorCommand(%q) = %t", command, got)
		}
		if got := isTerminalMultiplexerCommand(command); got != expected.multiplexer {
			t.Errorf("isTerminalMultiplexerCommand(%q) = %t", command, got)
		}
	}
}

func TestTerminalParserMetricsTrackCloseOnce(t *testing.T) {
	terminal, err := terminalparser.New(terminalparser.WithMaxScrollback(0))
	if err != nil {
		t.Fatal(err)
	}
	before := GetTerminalParserMetrics().Active
	activeTerminalParsers.Add(1)
	parser := TerminalParser{Terminal: terminal, counted: true}

	if got := GetTerminalParserMetrics().Active; got != before+1 {
		t.Fatalf("active terminal parsers = %d, want %d", got, before+1)
	}
	if err := parser.Close(); err != nil {
		t.Fatal(err)
	}
	if err := parser.Close(); err != nil {
		t.Fatal(err)
	}
	if got := GetTerminalParserMetrics().Active; got != before {
		t.Fatalf("active terminal parsers after close = %d, want %d", got, before)
	}
}

func TestIsTerminalMultiplexerCommand(t *testing.T) {
	tests := map[string]bool{
		"tmux":                          true,
		"sudo tmux attach-session":      true,
		"TERM=xterm-256color screen -x": true,
		"/usr/bin/tmux new-session":     true,
		"vim file":                      false,
		"echo tmux":                     false,
	}
	for command, expected := range tests {
		if got := isTerminalMultiplexerCommand(command); got != expected {
			t.Errorf("isTerminalMultiplexerCommand(%q) = %t", command, got)
		}
	}
}

func TestParseSrvOutputUsesCurrentWindowSize(t *testing.T) {
	terminal, err := terminalparser.New(
		terminalparser.WithSize(10, 3),
		terminalparser.WithMaxScrollback(0),
	)
	if err != nil {
		t.Fatal(err)
	}
	parser := TerminalParser{Terminal: terminal, width: 10, height: 3}
	t.Cleanup(func() { _ = parser.Close() })
	parser.srvOutputBuf.WriteString("123456789012345")

	rows := parser.parseSrvOutputRows()
	if len(rows) != 2 || rows[0] != "1234567890" || rows[1] != "12345" {
		t.Fatalf("parseSrvOutputRows() = %#v", rows)
	}
}

func TestMultipleCommandsUseBoundedOutputBufferWithoutScrollback(t *testing.T) {
	terminal, err := terminalparser.New(
		terminalparser.WithSize(80, 4),
		terminalparser.WithMaxScrollback(0),
	)
	if err != nil {
		t.Fatal(err)
	}
	parser := TerminalParser{
		Terminal: terminal,
		Ps1sStr:  "$",
		commands: []string{"echo one", "echo two", ""},
		width:    80,
		height:   4,
	}
	t.Cleanup(func() { _ = parser.Close() })
	parser.srvOutputBuf.WriteString("$ echo one\r\none\r\n$ echo two\r\ntwo\r\n$ ")

	type emittedCommand struct {
		command string
		output  string
	}
	var emitted []emittedCommand
	parser.EmitCommands = func(command, output string) {
		emitted = append(emitted, emittedCommand{command: command, output: output})
	}
	parser.TryMultipleCommands()

	if len(emitted) != 2 {
		t.Fatalf("emitted commands = %#v", emitted)
	}
	if emitted[0].command != "echo two" || strings.TrimSpace(emitted[0].output) != "two" {
		t.Fatalf("first emitted command = %#v", emitted[0])
	}
	if emitted[1].command != "echo one" || strings.TrimSpace(emitted[1].output) != "one" {
		t.Fatalf("second emitted command = %#v", emitted[1])
	}
	if parser.srvOutputBuf.Len() != 0 {
		t.Fatalf("output buffer retained %d bytes", parser.srvOutputBuf.Len())
	}
}

func TestZmodemStartFramesBypassTerminalParser(t *testing.T) {
	tests := []struct {
		name   string
		header string
		status string
	}{
		{name: "sz download", header: "0000000000000000", status: zmodem.ZParserStatusSend},
		{name: "rz upload", header: "0100000000000000", status: zmodem.ZParserStatusReceive},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			terminal, err := terminalparser.New(
				terminalparser.WithSize(80, 24),
				terminalparser.WithMaxScrollback(0),
			)
			if err != nil {
				t.Fatal(err)
			}
			terminalParser := &TerminalParser{Terminal: terminal, width: 80, height: 24}
			t.Cleanup(func() { _ = terminalParser.Close() })
			parser := Parser{
				TerminalParser: terminalParser,
				zmodemParser:   zmodem.New(),
			}

			packet := []byte("visible prefix**\x18B" + tt.header + "\r\npayload")
			if forwarded := parser.splitCmdStream(packet); !bytes.Equal(forwarded, packet) {
				t.Fatalf("forwarded packet changed: %x", forwarded)
			}
			if got := parser.zmodemParser.Status(); got != tt.status {
				t.Fatalf("zmodem status = %q, want %q", got, tt.status)
			}
			if got := terminalParser.GetCursorRow(); got != "visible prefix" {
				t.Fatalf("terminal row = %q, protocol payload was not filtered", got)
			}
			if terminalParser.srvOutputBuf.Len() != 0 {
				t.Fatalf("zmodem packet reached output buffer: %d bytes", terminalParser.srvOutputBuf.Len())
			}
		})
	}
}

func TestFragmentedZmodemStartFrameBypassesTerminalParser(t *testing.T) {
	terminal, err := terminalparser.New(
		terminalparser.WithSize(80, 24),
		terminalparser.WithMaxScrollback(0),
	)
	if err != nil {
		t.Fatal(err)
	}
	terminalParser := &TerminalParser{Terminal: terminal, width: 80, height: 24}
	t.Cleanup(func() { _ = terminalParser.Close() })
	parser := Parser{
		TerminalParser: terminalParser,
		zmodemParser:   zmodem.New(),
	}

	parser.splitCmdStream([]byte("before frame**"))
	if parser.zmodemParser.IsStartSession() {
		t.Fatal("partial zmodem prefix started a session")
	}
	if got := terminalParser.GetCursorRow(); got != "before frame" {
		t.Fatalf("terminal row after partial prefix = %q", got)
	}

	parser.splitCmdStream([]byte("\x18B0000000000000000\r\npayload"))
	if got := parser.zmodemParser.Status(); got != zmodem.ZParserStatusSend {
		t.Fatalf("zmodem status = %q", got)
	}
	if got := terminalParser.GetCursorRow(); got != "before frame" {
		t.Fatalf("fragmented protocol payload reached terminal parser: %q", got)
	}
}

func TestCmdParser_Parse(t *testing.T) {
	var b = []byte("ifconfig \x08\x1b[K\x08\x1b[K\x08\x1b[K\x08\x1b[K\x08\x1b[K\x08\x1b[Konfig")
	data, err := terminalparser.Parse(b)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(data, "") != "ifconfig" {
		t.Error("data should be ifconfig but not", data)
	}

	b = []byte("ifconfig\xe4\xbd\xa0")
	data, err = terminalparser.Parse(b)
	if err != nil {
		t.Fatal(err)
	}
	t.Log("line: ", strings.Join(data, ""))

}

func TestIsTmuxStatusBar(t *testing.T) {
	bar := []byte{
		0x1b, 0x5b, 0x3f, 0x32, 0x35, 0x6c, 0x1b, 0x5b, 0x33, 0x30, 0x6d, 0x1b, 0x5b, 0x34, 0x32, 0x6d,
		0x1b, 0x5b, 0x33, 0x39, 0x3b, 0x31, 0x48, 0x5b, 0x36, 0x30, 0x5d, 0x20, 0x30, 0x3a, 0x62, 0x61,
		0x73, 0x68, 0x2a, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20,
		0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20,
		0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20,
		0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20,
		0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20,
		0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x22, 0x6a, 0x75, 0x6d, 0x70, 0x73, 0x65, 0x72,
		0x76, 0x65, 0x72, 0x22, 0x20, 0x30, 0x37, 0x3a, 0x33, 0x39, 0x20, 0x30, 0x38, 0x2d, 0x41, 0x75,
		0x67, 0x2d, 0x32, 0x35, 0x1b, 0x28, 0x42, 0x1b, 0x5b, 0x6d, 0x1b, 0x5b, 0x33, 0x3b, 0x32, 0x30,
		0x48, 0x1b, 0x5b, 0x3f, 0x31, 0x32, 0x6c, 0x1b, 0x5b, 0x3f, 0x32, 0x35, 0x68,
	}
	hasBar := tmuxBarRegx.Match(bar)
	hasBar2 := tmuxBar2Regx.Match(bar)
	t.Logf("hasbar: %v", hasBar)
	t.Logf("hasbar2: %v", hasBar2)

}

func TestIsPasswordPrompt(t *testing.T) {
	prompts := []string{
		"password:",
		"[sudo] password for eric:",
	}
	for _, prompt := range prompts {
		if !IsPasswordPrompt(prompt) {
			t.Error(prompt)
		}

	}
}
