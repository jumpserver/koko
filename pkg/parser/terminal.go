package parser

import (
	"bytes"
	"sync"
)

func NewTerminalParser() *TerminalParser {
	return &TerminalParser{
		Once:         sync.Once{},
		Started:      false,
		InputStatus:  false,
		OutputStatus: false,
		CmdInputBuf:  new(bytes.Buffer),
		CmdOutputBuf: new(bytes.Buffer),
	}
}

type TerminalParser struct {
	Once         sync.Once
	Started      bool
	InputStatus  bool
	OutputStatus bool

	CmdInputBuf  *bytes.Buffer // node对用户输入的回写数据
	CmdOutputBuf *bytes.Buffer // node对用户按下enter按键之后，返回的数据

}

func (t *TerminalParser) Reset() {
	t.CmdInputBuf.Reset()
	t.CmdOutputBuf.Reset()
}

func (t *TerminalParser) ParseCommandInput() string {
	return t.CmdInputBuf.String()
}

func (t *TerminalParser) ParseCommandResult() string {
	return t.CmdOutputBuf.String()
}

func (t *TerminalParser) IsEnterKey(b []byte) bool {
	return len(b) == 1 && b[0] == 13
}
