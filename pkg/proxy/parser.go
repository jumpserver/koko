package proxy

import (
	"bytes"
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/LeeEirc/tclientlib"

	"github.com/jumpserver/koko/pkg/exchange"
	"github.com/jumpserver/koko/pkg/i18n"
	"github.com/jumpserver/koko/pkg/jms-sdk-go/model"
	"github.com/jumpserver/koko/pkg/jms-sdk-go/service"
	"github.com/jumpserver/koko/pkg/logger"
	"github.com/jumpserver/koko/pkg/utils"
)

var (
	charEnter = []byte("\r")

	enterMarks = [][]byte{
		[]byte("\x1b[?1049h"),
		[]byte("\x1b[?1048h"),
		[]byte("\x1b[?1047h"),
		[]byte("\x1b[?47h"),
	}

	exitMarks = [][]byte{
		[]byte("\x1b[?1049l"),
		[]byte("\x1b[?1048l"),
		[]byte("\x1b[?1047l"),
		[]byte("\x1b[?47l"),
	}
)

const (
	CommandInputParserName  = "Command Input parser"
	CommandOutputParserName = "Command Output parser"
)

var _ ParseEngine = (*Parser)(nil)

type Parser struct {
	id           string
	protocolType string
	jmsService   *service.JMService

	userOutputChan chan []byte
	srvOutputChan  chan []byte
	cmdRecordChan  chan *ExecutedCommand

	inputInitial  bool
	inputPreState bool
	inputState    bool

	inVimState bool
	once       *sync.Once
	lock       *sync.RWMutex

	command         string
	output          string
	cmdCreateDate   time.Time
	cmdInputParser  *CmdParser
	cmdOutputParser *CmdParser

	cmdFilterRules []model.SystemUserFilterRule
	closed         chan struct{}

	confirmStatus commandConfirmStatus

	zmodemParser        *ZmodemParser
	permAction          *model.Permission
	enableDownload      bool
	enableUpload        bool
	abortedFileTransfer bool
}

func (p *Parser) initial() {
	p.once = new(sync.Once)
	p.lock = new(sync.RWMutex)

	p.cmdInputParser = NewCmdParser(p.id, CommandInputParserName)
	p.cmdOutputParser = NewCmdParser(p.id, CommandOutputParserName)
	p.closed = make(chan struct{})
	p.cmdRecordChan = make(chan *ExecutedCommand, 1024)
}

// ParseStream 解析数据流
func (p *Parser) ParseStream(userInChan chan *exchange.RoomMessage, srvInChan <-chan []byte) (userOut, srvOut <-chan []byte) {
	p.userOutputChan = make(chan []byte, 1)
	p.srvOutputChan = make(chan []byte, 1)
	logger.Infof("Session %s: Parser start", p.id)
	go func() {
		defer func() {
			// 会话结束，结算命令结果
			p.sendCommandRecord()
			close(p.cmdRecordChan)
			close(p.userOutputChan)
			close(p.srvOutputChan)
			p.zmodemParser.Cleanup()
			logger.Infof("Session %s: Parser routine done", p.id)
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
				if len(b) == 0 {
					continue
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
func (p *Parser) parseInputState(b []byte) []byte {
	if p.zmodemParser.IsStartSession() {
		switch p.zmodemParser.Status() {
		case ZParserStatusReceive:
			p.zmodemParser.Parse(b)
			if p.zmodemParser.IsZFilePacket() && !p.enableUpload {
				logger.Infof("Send zmodem user skip and srv abort to disable upload")
				p.abortedFileTransfer = true
				// 不记录中断的文件
				p.zmodemParser.setAbortMark()
				p.srvOutputChan <- skipSequence
				return AbortSession
			}

			if !p.zmodemParser.IsStartSession() && p.abortedFileTransfer {
				/*
					使用 zskip 中断文件上传之后，user 会发送 zfin 表示结束.
					此时，因为 srv 端已经中断，则不应接受 zmodem 字符，可以发nil
				*/

				logger.Info("Zmodem abort upload file finished")
				msg := i18n.T("have no permission to upload file")
				p.abortedFileTransfer = false
				p.srvOutputChan <- CancelSequence
				p.srvOutputChan <- []byte("\r\n")
				p.srvOutputChan <- []byte(msg)
				p.srvOutputChan <- []byte("\r\n")
				return charEnter
			}
		case ZParserStatusSend:
			if p.zmodemParser.IsZFilePacket() && !p.enableDownload {
				logger.Infof("Send zmodem srv skip and user abort to disable download")
				p.abortedFileTransfer = true
				p.userOutputChan <- AbortSession
				// 不记录中断的文件
				p.zmodemParser.setAbortMark()
				return charEnter
			}
		default:
		}
		return b
	}
	if !p.IsNeedParse() {
		return b
	}

	if p.confirmStatus.InRunning() {
		if p.confirmStatus.IsNeedCancel(b) {
			logger.Infof("Session %s: user cancel confirm status", p.id)
			p.srvOutputChan <- []byte("\r\n")
			return nil
		}
		logger.Infof("Session %s: command confirm status %s, drop input", p.id,
			p.confirmStatus.Status)
		return nil
	}
	waitMsg := i18n.T("the reviewers will confirm. continue or not [Y/n]")
	if p.confirmStatus.InQuery() {
		switch strings.ToLower(string(b)) {
		case "y":
			p.confirmStatus.SetStatus(StatusStart)
			p.confirmStatus.wg.Add(1)
			go func() {
				p.confirmStatus.SetAction(model.ActionUnknown)
				p.waitCommandConfirm()
				processor := p.confirmStatus.GetProcessor()
				switch p.confirmStatus.GetAction() {
				case model.ActionAllow:
					formatMsg := i18n.T("%s approved")
					statusMsg := utils.WrapperString(fmt.Sprintf(formatMsg, processor), utils.Green)
					p.srvOutputChan <- []byte("\r\n")
					p.srvOutputChan <- []byte(statusMsg)
					p.userOutputChan <- []byte(p.confirmStatus.data)
				case model.ActionDeny:
					formatMsg := i18n.T("%s rejected")
					statusMsg := utils.WrapperString(fmt.Sprintf(formatMsg, processor), utils.Red)
					p.srvOutputChan <- []byte("\r\n")
					p.srvOutputChan <- []byte(statusMsg)
					p.forbiddenCommand(p.confirmStatus.Cmd)
				default:
					// 默认是取消 不执行
					p.srvOutputChan <- []byte("\r\n")
					p.userOutputChan <- breakInputPacket(p.protocolType)
				}
				// 审核结束, 重置状态
				p.confirmStatus.SetStatus(StatusNone)
				p.confirmStatus.wg.Done()
			}()
		case "n":
			p.confirmStatus.SetStatus(StatusNone)
			p.srvOutputChan <- []byte("\r\n")
			return breakInputPacket(p.protocolType)
		default:
			p.srvOutputChan <- []byte("\r\n" + waitMsg)
		}
		return nil
	}

	if bytes.LastIndex(b, charEnter) == 0 {
		// 连续输入enter key, 结算上一条可能存在的命令结果
		p.sendCommandRecord()
		p.inputState = false
		// 用户输入了Enter，开始结算命令
		p.parseCmdInput()
		if rule, cmd, ok := p.IsMatchCommandRule(p.command); ok {
			switch rule.Action {
			case model.ActionDeny:
				p.forbiddenCommand(cmd)
				return nil
			case model.ActionConfirm:
				p.confirmStatus.SetStatus(StatusQuery)
				p.confirmStatus.SetRule(rule)
				p.confirmStatus.SetCmd(cmd)
				p.confirmStatus.SetData(string(b))
				p.confirmStatus.ResetCtx()
				p.srvOutputChan <- []byte("\r\n" + waitMsg)
				return nil
			default:
			}
		}
	} else {
		p.inputState = true
		// 用户又开始输入，并上次不处于输入状态，开始结算上次命令的结果
		if !p.inputPreState {
			p.sendCommandRecord()
			if ps1 := p.cmdOutputParser.GetPs1(); ps1 != "" {
				p.cmdInputParser.SetPs1(ps1)
			}
		}
	}
	return b
}

func (p *Parser) IsNeedParse() bool {
	p.lock.Lock()
	defer p.lock.Unlock()
	if p.inVimState {
		return false
	}
	p.inputPreState = p.inputState
	return true
}

func (p *Parser) forbiddenCommand(cmd string) {
	fbdMsg := utils.WrapperWarn(fmt.Sprintf(i18n.T("Command `%s` is forbidden"), cmd))
	p.srvOutputChan <- []byte("\r\n" + fbdMsg)
	p.cmdRecordChan <- &ExecutedCommand{
		Command:     p.command,
		Output:      fbdMsg,
		CreatedDate: p.cmdCreateDate,
		RiskLevel:   model.HighRiskFlag}
	p.command = ""
	p.output = ""
	p.userOutputChan <- breakInputPacket(p.protocolType)
}

// parseCmdInput 解析命令的输入
func (p *Parser) parseCmdInput() {
	commands := p.cmdInputParser.Parse()
	if len(commands) <= 0 {
		p.command = ""
	} else {
		p.command = commands[len(commands)-1]
	}
	p.cmdCreateDate = time.Now()
}

// parseCmdOutput 解析命令输出
func (p *Parser) parseCmdOutput() {
	p.output = strings.Join(p.cmdOutputParser.Parse(), "\r\n")
}

// ParseUserInput 解析用户的输入
func (p *Parser) ParseUserInput(b []byte) []byte {
	p.once.Do(func() {
		p.inputInitial = true
	})
	nb := p.parseInputState(b)
	return nb
}

// parseZmodemState 解析数据，查看是不是处于zmodem状态
// 处于zmodem状态不会再解析命令
func (p *Parser) parseZmodemState(b []byte) {
	p.zmodemParser.Parse(b)
}

// parseVimState 解析vim的状态，处于vim状态中，里面输入的命令不再记录
func (p *Parser) parseVimState(b []byte) {
	if !p.inVimState && IsEditEnterMode(b) {
		p.inVimState = true
		logger.Debug("In vim state: true")
	}
	if p.inVimState && IsEditExitMode(b) {
		p.inVimState = false
		logger.Debug("In vim state: false")
	}
}

// splitCmdStream 将服务器输出流分离到命令buffer和命令输出buffer
func (p *Parser) splitCmdStream(b []byte) []byte {
	if p.zmodemParser.IsStartSession() {
		if p.zmodemParser.Status() == ZParserStatusSend {
			p.zmodemParser.Parse(b)
		}
		if !p.zmodemParser.IsStartSession() && p.abortedFileTransfer {
			logger.Info("Zmodem abort download file finished")
			p.abortedFileTransfer = false
			p.srvOutputChan <- b
			msg := i18n.T("have no permission to download file")
			p.srvOutputChan <- []byte("\r\n")
			p.srvOutputChan <- []byte(msg)
			p.srvOutputChan <- []byte("\r\n")
			p.userOutputChan <- charEnter
			return nil
		}
		return b
	} else {
		p.parseVimState(b)
		if p.inVimState || !p.inputInitial {
			return b
		}
		p.parseZmodemState(b)
	}
	if p.zmodemParser.IsStartSession() {
		logger.Infof("Zmodem start session %s", p.zmodemParser.Status())
		return b
	}
	if p.inputState {
		_, _ = p.cmdInputParser.WriteData(b)
	}
	_, _ = p.cmdOutputParser.WriteData(b)
	return b
}

// ParseServerOutput 解析服务器输出
func (p *Parser) ParseServerOutput(b []byte) []byte {
	p.lock.Lock()
	defer p.lock.Unlock()
	return p.splitCmdStream(b)
}

// SetCMDFilterRules 设置命令过滤规则
func (p *Parser) SetCMDFilterRules(rules []model.SystemUserFilterRule) {
	// 优先级排序
	sort.Sort(model.FilterRules(rules))
	p.cmdFilterRules = rules
}

// IsMatchCommandRule 判断命令是不是在过滤规则中
func (p *Parser) IsMatchCommandRule(command string) (model.SystemUserFilterRule, string, bool) {
	for _, rule := range p.cmdFilterRules {
		allowed, cmd := rule.Match(command)
		switch allowed {
		case model.ActionAllow:
			return rule, cmd, true
		case model.ActionConfirm, model.ActionDeny:
			return rule, cmd, true
		default:
		}
	}
	return model.SystemUserFilterRule{}, "", false
}

func (p *Parser) waitCommandConfirm() {
	cmd := p.confirmStatus.Cmd
	resp, err := p.jmsService.SubmitCommandConfirm(p.id, p.confirmStatus.Rule.ID, p.confirmStatus.Cmd)
	if err != nil {
		logger.Errorf("Session %s: submit command confirm api err: %s", p.id, err)
		p.confirmStatus.SetAction(model.ActionDeny)
		return
	}
	checkReq := resp.CheckConfirmStatus
	cancelReq := resp.CloseConfirm
	detailURL := resp.TicketDetailUrl
	reviewers := resp.Reviewers
	msg := i18n.T("Please waiting for the reviewers to confirm command `%s`, cancel by CTRL+C.")
	waitMsg := fmt.Sprintf(msg, cmd)
	checkTimer := time.NewTicker(10 * time.Second)
	defer checkTimer.Stop()
	ctx, cancelFunc := context.WithCancel(p.confirmStatus.ctx)
	defer cancelFunc()
	go func() {
		delay := 0
		titleMsg := i18n.T("Need ticket confirm to execute command, already send email to the reviewers")
		reviewersMsg := fmt.Sprintf(i18n.T("Ticket Reviewers: %s"), strings.Join(reviewers, ", "))
		detailURLMsg := fmt.Sprintf(i18n.T("Could copy website URL to notify reviewers: %s"), detailURL)
		var tipString strings.Builder
		tipString.WriteString(utils.CharNewLine)
		tipString.WriteString(titleMsg)
		tipString.WriteString(utils.CharNewLine)
		tipString.WriteString(reviewersMsg)
		tipString.WriteString(utils.CharNewLine)
		tipString.WriteString(detailURLMsg)
		tipString.WriteString(utils.CharNewLine)
		p.srvOutputChan <- []byte(utils.WrapperString(tipString.String(), utils.Green))
		for {
			select {
			case <-p.closed:
				return
			case <-ctx.Done():
				return
			default:
				delayS := fmt.Sprintf("%ds", delay)
				data := strings.Repeat("\x08", len(delayS)+len(waitMsg)) + waitMsg + delayS
				p.srvOutputChan <- []byte(data)
				time.Sleep(time.Second)
				delay += 1
			}
		}
	}()
	for {
		select {
		case <-p.closed:
			if err = p.jmsService.CancelConfirmByRequestInfo(cancelReq); err != nil {
				logger.Errorf("Session %s: Cancel command confirm err: %s", p.id, err)
			}
			logger.Infof("Session %s: Closed", p.id)
			return
		case <-ctx.Done():
			// 取消
			if err = p.jmsService.CancelConfirmByRequestInfo(cancelReq); err != nil {
				logger.Errorf("Session %s: Cancel command confirm err: %s", p.id, err)
			}
			logger.Infof("Session %s: Cancel confirm command", p.id)
			return
		case <-checkTimer.C:
		}
		statusResp, err := p.jmsService.CheckConfirmStatusByRequestInfo(checkReq)
		if err != nil {
			logger.Errorf("Session %s: check command confirm status err: %s", p.id, err)
			continue
		}
		switch statusResp.Status {
		case approve:
			p.confirmStatus.SetAction(model.ActionAllow)
			p.confirmStatus.SetProcessor(statusResp.Processor)
			return
		case reject:
			p.confirmStatus.SetProcessor(statusResp.Processor)
			p.confirmStatus.SetAction(model.ActionDeny)
			return
		case await:
			continue
		default:
			logger.Errorf("Receive unknown command confirm status %s", statusResp.Status)
		}
	}
}

const (
	approve = "approve"
	reject  = "reject"
	await   = "await"
)

func (p *Parser) IsInZmodemRecvState() bool {
	return p.zmodemParser.IsStartSession()
}

// Close 关闭parser
func (p *Parser) Close() {
	select {
	case <-p.closed:
		return
	default:
		close(p.closed)

	}
	_ = p.cmdOutputParser.Close()
	_ = p.cmdInputParser.Close()
	logger.Infof("Session %s: Parser close", p.id)
}

func (p *Parser) sendCommandRecord() {
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

func (p *Parser) NeedRecord() bool {
	return !p.IsInZmodemRecvState()
}

func (p *Parser) CommandRecordChan() chan *ExecutedCommand {
	return p.cmdRecordChan
}

type ExecutedCommand struct {
	Command     string
	Output      string
	CreatedDate time.Time
	RiskLevel   string
}

func IsEditEnterMode(p []byte) bool {
	return matchMark(p, enterMarks)
}

func IsEditExitMode(p []byte) bool {
	return matchMark(p, exitMarks)
}

func matchMark(p []byte, marks [][]byte) bool {
	for _, item := range marks {
		if bytes.Contains(p, item) {
			return true
		}
	}
	return false
}

func breakInputPacket(protocolType string) []byte {
	switch protocolType {
	case model.ProtocolTelnet:
		return []byte{tclientlib.IAC, tclientlib.BRK, '\r'}
	case model.ProtocolSSH:
		return []byte{utils.CharCleanLine, '\r'}
	}
	return []byte{utils.CharCleanLine, '\r'}
}
