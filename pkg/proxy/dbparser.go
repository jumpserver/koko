package proxy

import (
	"bytes"
	"fmt"
	"sync"

	"github.com/jumpserver/koko/pkg/i18n"
	"github.com/jumpserver/koko/pkg/logger"
	"github.com/jumpserver/koko/pkg/model"
	"github.com/jumpserver/koko/pkg/utils"
)

const (
	DBInputParserName  = "DB Input parser"
	DBOutputParserName = "DB Output parser"
)

func newDBParser(id string) DBParser {
	dbParser := DBParser{
		id: id,
	}
	dbParser.initial()
	return dbParser
}

type DBParser struct {
	id string

	userOutputChan chan []byte
	srvOutputChan  chan []byte
	cmdRecordChan  chan [2]string

	inputInitial  bool
	inputPreState bool
	inputState    bool
	once          *sync.Once
	lock          *sync.RWMutex

	command         string
	output          string
	cmdInputParser  *CmdParser
	cmdOutputParser *CmdParser

	cmdFilterRules []model.SystemUserFilterRule
	closed         chan struct{}
}

func (p *DBParser) initial() {
	p.once = new(sync.Once)
	p.lock = new(sync.RWMutex)

	p.cmdInputParser = NewCmdParser(p.id, DBInputParserName)
	p.cmdOutputParser = NewCmdParser(p.id, DBOutputParserName)

	p.closed = make(chan struct{})
	p.cmdRecordChan = make(chan [2]string, 1024)
}

// ParseStream 解析数据流
func (p *DBParser) ParseStream(userInChan, srvInChan <-chan []byte) (userOut, srvOut <-chan []byte) {

	p.userOutputChan = make(chan []byte, 1)
	p.srvOutputChan = make(chan []byte, 1)
	logger.Infof("DB Session %s: Parser start", p.id)
	go func() {
		defer func() {
			// 会话结束，结算命令结果
			p.sendCommandRecord()
			close(p.cmdRecordChan)
			close(p.userOutputChan)
			close(p.srvOutputChan)
			logger.Infof("DB Session %s: Parser routine done", p.id)
		}()
		for {
			select {
			case <-p.closed:
				return
			case b, ok := <-userInChan:
				if !ok {
					return
				}
				b = p.ParseUserInput(b)
				select {
				case <-p.closed:
					return
				case p.userOutputChan <- b:
				}

			case b, ok := <-srvInChan:
				if !ok {
					return
				}
				b = p.ParseServerOutput(b)
				select {
				case <-p.closed:
					return
				case p.srvOutputChan <- b:
				}

			}
		}
	}()
	return p.userOutputChan, p.srvOutputChan
}

// parseInputState 切换用户输入状态, 并结算命令和结果
func (p *DBParser) parseInputState(b []byte) []byte {
	p.inputPreState = p.inputState
	if bytes.Contains(b, charEnter) {
		// 连续输入enter key, 结算上一条可能存在的命令结果
		p.sendCommandRecord()
		p.inputState = false
		// 用户输入了Enter，开始结算命令
		p.parseCmdInput()
		if cmd, ok := p.IsCommandForbidden(); !ok {
			fbdMsg := utils.WrapperWarn(fmt.Sprintf(i18n.T("Command `%s` is forbidden"), cmd))
			_, _ = p.cmdOutputParser.WriteData([]byte(fbdMsg))
			p.srvOutputChan <- []byte("\r\n" + fbdMsg)
			p.cmdRecordChan <- [2]string{p.command, fbdMsg}
			p.command = ""
			p.output = ""
			return []byte{utils.CharCleanLine, '\r'}
		}
	} else {
		p.inputState = true
		// 用户又开始输入，并上次不处于输入状态，开始结算上次命令的结果
		if !p.inputPreState {
			p.sendCommandRecord()
		}
	}
	return b
}

// parseCmdInput 解析命令的输入
func (p *DBParser) parseCmdInput() {
	p.command = p.cmdInputParser.Parse()
}

// parseCmdOutput 解析命令输出
func (p *DBParser) parseCmdOutput() {
	p.output = p.cmdOutputParser.Parse()
}

// ParseUserInput 解析用户的输入
func (p *DBParser) ParseUserInput(b []byte) []byte {
	p.lock.Lock()
	defer p.lock.Unlock()
	p.once.Do(func() {
		p.inputInitial = true
	})
	nb := p.parseInputState(b)
	return nb
}

// splitCmdStream 将服务器输出流分离到命令buffer和命令输出buffer
func (p *DBParser) splitCmdStream(b []byte) {
	if !p.inputInitial {
		return
	}
	if p.inputState {
		_, _ = p.cmdInputParser.WriteData(b)
		return
	}
	_, _ = p.cmdOutputParser.WriteData(b)
}

// ParseServerOutput 解析服务器输出
func (p *DBParser) ParseServerOutput(b []byte) []byte {
	p.lock.Lock()
	defer p.lock.Unlock()
	p.splitCmdStream(b)
	return b
}

// SetCMDFilterRules 设置命令过滤规则
func (p *DBParser) SetCMDFilterRules(rules []model.SystemUserFilterRule) {
	p.cmdFilterRules = rules
}

// IsCommandForbidden 判断命令是不是在过滤规则中
func (p *DBParser) IsCommandForbidden() (string, bool) {
	for _, rule := range p.cmdFilterRules {
		allowed, cmd := rule.Match(p.command)
		switch allowed {
		case model.ActionAllow:
			return "", true
		case model.ActionDeny:
			return cmd, false
		default:

		}
	}
	return "", true
}

// Close 关闭parser
func (p *DBParser) Close() {
	select {
	case <-p.closed:
		return
	default:
		close(p.closed)
	}
	_ = p.cmdOutputParser.Close()
	_ = p.cmdInputParser.Close()
	logger.Infof("DB Session %s: Parser close", p.id)
}

func (p *DBParser) sendCommandRecord() {
	if p.command != "" {
		p.parseCmdOutput()
		p.cmdRecordChan <- [2]string{p.command, p.output}
		p.command = ""
		p.output = ""
	}
}
