package proxy

import (
	"bytes"
	"fmt"
	"sync"

	"cocogo/pkg/logger"
	"cocogo/pkg/model"
)

type ParseRule func([]byte) bool

var (
	// Todo: Vim过滤依然存在问题
	vimEnterMark = []byte("\x1b[?25l\x1b[37;1H\x1b[1m")
	vimExitMark  = []byte("\x1b[37;1H\x1b[K\x1b")

	zmodemRecvStartMark = []byte("rz waiting to receive.**\x18B0100")
	zmodemSendStartMark = []byte("**\x18B00000000000000")
	zmodemCancelMark    = []byte("\x18\x18\x18\x18\x18")
	zmodemEndMark       = []byte("**\x18B0800000000022d")
	zmodemStateSend     = "send"
	zmodemStateRecv     = "recv"

	charEnter = []byte("\r")
)

// Parse 解析用户输入输出, 拦截过滤用户输入输出
type Parser struct {
	inputBuf  *bytes.Buffer
	cmdBuf    *bytes.Buffer
	outputBuf *bytes.Buffer

	userInputChan   chan []byte
	serverInputChan chan []byte

	filterRules []model.SystemUserFilterRule

	inputInitial    bool
	inputPreState   bool
	inputState      bool
	multiInputState bool
	zmodemState     string
	inVimState      bool
	once            sync.Once
}

// Todo: parseMultipleInput 依然存在问题

// parseInputState 切换用户输入状态
func (p *Parser) parseInputState(b []byte) {
	if p.inVimState || p.zmodemState != "" {
		return
	}
	p.inputPreState = p.inputState
	if bytes.Contains(b, charEnter) {
		p.inputState = false
		//fmt.Printf("Command: %s\n", p.inputBuf.String())
		p.inputBuf.Reset()
	} else {
		p.inputState = true
		if !p.inputPreState {
			//fmt.Printf("Output: %s\n", p.outputBuf.String())
			p.outputBuf.Reset()
		}
	}
}

func (p *Parser) parseInputNewLine(b []byte) []byte {
	b = bytes.Replace(b, []byte{'\r', '\r', '\n'}, []byte{'\r'}, -1)
	b = bytes.Replace(b, []byte{'\r', '\n'}, []byte{'\r'}, -1)
	b = bytes.Replace(b, []byte{'\n'}, []byte{'\r'}, -1)
	return b
}

func (p *Parser) ParseUserInput(b []byte) []byte {
	p.once.Do(func() {
		p.inputInitial = true
	})
	nb := p.parseInputNewLine(b)
	p.inputBuf.Write(nb)
	fmt.Printf("User input: %b\n", b)
	p.parseInputState(nb)
	return b
}

func (p *Parser) parseVimState(b []byte) {
	if p.zmodemState == "" && !p.inVimState && bytes.Contains(b, vimEnterMark) {
		p.inVimState = true
		logger.Debug("In vim state: true")
	}
	if p.zmodemState == "" && p.inVimState && bytes.Contains(b, vimExitMark) {
		p.inVimState = false
		logger.Debug("In vim state: false")
	}
}

func (p *Parser) parseZmodemState(b []byte) {
	if p.zmodemState == "" {
		if bytes.Contains(b[:50], zmodemRecvStartMark) {
			p.zmodemState = zmodemStateRecv
			logger.Debug("Zmodem in recv state")
		} else if bytes.Contains(b[:24], zmodemSendStartMark) {
			p.zmodemState = zmodemStateSend
			logger.Debug("Zmodem in send state")
		}
	} else {
		if bytes.Contains(b[:24], zmodemEndMark) {
			logger.Debug("Zmodem end")
			p.zmodemState = ""
		} else if bytes.Contains(b[:24], zmodemCancelMark) {
			logger.Debug("Zmodem cancel")
			p.zmodemState = ""
		}
	}
}

func (p *Parser) parseCommand(b []byte) {
	if p.inputState {
		p.cmdBuf.Write(b)
	} else {
		p.outputBuf.Write(b)
	}
}

func (p *Parser) ParseServerOutput(b []byte) []byte {
	p.parseVimState(b)
	p.parseZmodemState(b)
	p.parseCommand(b)
	fmt.Printf("Server output: %s\n", b)
	return b
}
