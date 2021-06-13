package proxy

import (
	"bytes"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/jumpserver/koko/pkg/exchange"
	"github.com/jumpserver/koko/pkg/i18n"
	"github.com/jumpserver/koko/pkg/jms-sdk-go/model"
	"github.com/jumpserver/koko/pkg/logger"
	"github.com/jumpserver/koko/pkg/utils"
)

const (
	DBInputParserName  = "DB Input parser"
	DBOutputParserName = "DB Output parser"
)

var _ ParseEngine = (*DBParser)(nil)

type DBParser struct {
	id string

	userOutputChan chan []byte
	srvOutputChan  chan []byte
	cmdRecordChan chan *ExecutedCommand

	inputInitial  bool
	inputPreState bool
	inputState    bool
	once          *sync.Once
	lock          *sync.RWMutex

	command         string
	output          string
	cmdCreateDate   time.Time
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
	p.cmdRecordChan = make(chan *ExecutedCommand, 1024)
}

// ParseStream 解析数据流
func (p *DBParser) ParseStream(userInChan chan *exchange.RoomMessage, srvInChan <-chan []byte) (userOut, srvOut <-chan []byte) {

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
			case msg, ok := <-userInChan:
				if !ok {
					return
				}
				var b []byte
				switch msg.Event {
				case exchange.DataEvent:
					b = msg.Body
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
			p.cmdRecordChan <- &ExecutedCommand{
				Command:     p.command,
				Output:      fbdMsg,
				CreatedDate: p.cmdCreateDate,
				RiskLevel:   model.HighRiskFlag}
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
	commands := p.cmdInputParser.Parse()
	if len(commands) <= 0 {
		p.command = ""
	} else {
		p.command = commands[len(commands)-1]
	}
}

// parseCmdOutput 解析命令输出
func (p *DBParser) parseCmdOutput() {
	p.output = strings.Join(p.cmdOutputParser.Parse(), "\r\n")
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
		p.cmdRecordChan <- &ExecutedCommand{
			Command:     p.command,
			Output:      p.output,
			CreatedDate: p.cmdCreateDate,
			RiskLevel:   model.LessRiskFlag}
		p.command = ""
		p.output = ""
	}
}

func (p *DBParser) NeedRecord() bool {
	return true
}

func (p *DBParser) CommandRecordChan() chan *ExecutedCommand{
	return p.cmdRecordChan
}
