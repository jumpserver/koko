package proxy

import (
	"bytes"
	"context"
	"fmt"

	"strings"
	"sync"
	"time"

	"github.com/LeeEirc/tclientlib"
	"github.com/LeeEirc/terminalparser"
	"github.com/jumpserver-dev/sdk-go/model"
	"github.com/jumpserver-dev/sdk-go/service"
	"github.com/jumpserver/koko/pkg/srvconn"

	"github.com/jumpserver/koko/pkg/config"
	"github.com/jumpserver/koko/pkg/exchange"
	"github.com/jumpserver/koko/pkg/i18n"
	"github.com/jumpserver/koko/pkg/logger"
	"github.com/jumpserver/koko/pkg/utils"
	"github.com/jumpserver/koko/pkg/zmodem"
)

var (
	charEnter = []byte("\r")
	charLF    = []byte("\n")

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
	screenMarks = [][]byte{
		{0x1b, 0x5b, 0x4b, 0x0d, 0x0a}, // 4b 0d 0a
		//{0x1b, 0x5b, 0x34, 0x6c}, // 1b 5b 34 6c
	}
	vimMarks = [][]byte{
		{0x1b, 0x5b, 0x32, 0x3b, 0x31}, // ESC ] 2;  设置标题 1b 5b 32 3b 31
		//{0x1b, 0x5b, 0x32, 0x32, 0x3b, 0x30, 0x3b, 0x30, 0x74}, // 1b 5b 32 32 3b 30 3b 30  74  设置标题的控制字符
	}
)

const (
	zmodemIdleTimeout          = 10 * time.Second
	zmodemSendHandshakeTimeout = 10 * time.Second
	zmodemInputDrainTimeout    = 2 * time.Second
<<<<<<< HEAD
	zmodemPromptRedrawDelay    = 500 * time.Millisecond
	zmodemMaxTransferSize      = int64(500 * 1024 * 1024)
	zmodemMaxTransferSizeLabel = "500 MB"
=======
	zmodemFinishTimeout        = 2 * time.Second
	zmodemPromptRedrawDelay    = 500 * time.Millisecond
	zmodemMaxTransferSize      = int64(500 * 1024 * 1024)
	zmodemMaxTransferSizeLabel = "500 MiB"
>>>>>>> origin/dev
	zmodemInterruptCtrlC       = byte(0x03)
)

type Parser struct {
	id           string
	protocolType string
	jmsService   *service.JMService

	userOutputChan chan []byte
	srvOutputChan  chan []byte
	cmdRecordChan  chan *ExecutedCommand

	TerminalParser *TerminalParser

	isScreenMode bool
	isEditMode   bool

	inVimState bool
	once       sync.Once
	modeLock   sync.RWMutex
	outputLock sync.Mutex

	command       string
	output        string
	cmdCreateDate time.Time

	cmdFilterACLs model.CommandACLs
	closed        chan struct{}

	confirmStatus commandConfirmStatus

	zmodemParser          *zmodem.ZmodemParser
<<<<<<< HEAD
	zmodemStartBuf        bytes.Buffer
	enableDownload        bool
	enableUpload          bool
	abortedFileTransfer   bool
	zmodemRejectMessage   string
	drainingZmodemInput   bool
	zmodemInputDrainUntil time.Time
=======
	enableDownload        bool
	enableUpload          bool
	abortedFileTransfer   bool
	oversizedFileTransfer bool
	zmodemRejectMessage   string
	drainingZmodemInput   bool
	zmodemInputDrainUntil time.Time
	awaitingZmodemOO      bool
	zmodemOOSeen          int
	zmodemOOUntil         time.Time
	zmodemOONotice        string
	awaitingZmodemSrvOO   bool
	zmodemSrvOOSeen       int
	zmodemSrvOOUntil      time.Time
	zmodemSrvOONotice     string
>>>>>>> origin/dev
	zmodemPromptRedraw    chan struct{}
	currentActiveUser     CurrentActiveUser

	i18nLang string

	platform *model.Platform

	currentCmdRiskLevel  int64
	currentCmdFilterRule CommandRule

	userInputFilter func([]byte) []byte
	terminalAIGrant func(string) (CommandACLDecision, bool)

	disableInputAsCmd bool
}

func (p *Parser) setCurrentCmdStatusLevel(level int64) {
	p.currentCmdRiskLevel = level
}

func (p *Parser) getCurrentCmdStatusLevel() int64 {
	return p.currentCmdRiskLevel
}

func (p *Parser) setCurrentCmdFilterRule(rule CommandRule) {
	p.currentCmdFilterRule = rule
}

func (p *Parser) getCurrentCmdFilterRule() CommandRule {
	return p.currentCmdFilterRule
}

func (p *Parser) resetCurrentCmdFilterRule() {
	p.currentCmdFilterRule = CommandRule{}
}

func (p *Parser) initial(w, h int) error {
	if w <= 0 || h <= 0 || w > 65535 || h > 65535 {
		return fmt.Errorf("create terminal parser: %w", terminalparser.ErrInvalidSize)
	}
	terminal, err := terminalparser.New(
		terminalparser.WithSize(uint16(w), uint16(h)),
		terminalparser.WithMaxScrollback(0),
	)
	if err != nil {
		return fmt.Errorf("create terminal parser: %w", err)
	}
	p.TerminalParser = &TerminalParser{
		IsEnter:      p.isEnterKeyPress,
		EmitCommands: p.EmitCommandEvent,
		Terminal:     terminal,
		width:        uint16(w),
		height:       uint16(h),
	}
	p.closed = make(chan struct{})
	p.cmdRecordChan = make(chan *ExecutedCommand, 1024)
	p.disableInputAsCmd = config.GetConf().DisableInputAsCommand
	return nil
}

func (p *Parser) SetUserInputFilter(filter func([]byte) []byte) {
	p.userInputFilter = filter
}

// ParseStream 解析数据流
func (p *Parser) ParseStream(userInChan chan *exchange.RoomMessage, srvInChan <-chan []byte) (userOut, srvOut <-chan []byte) {
	p.userOutputChan = make(chan []byte, 1)
	p.srvOutputChan = make(chan []byte, 1)
	p.zmodemPromptRedraw = make(chan struct{}, 1)
	logger.Infof("Session %s: Parser start", p.id)
	go func() {
		var (
			promptRedrawTimer  *time.Timer
			promptRedrawTimerC <-chan time.Time
		)
		defer func() {
			if promptRedrawTimer != nil {
				promptRedrawTimer.Stop()
			}
			// 会话结束，结算命令结果
			p.sendCommandRecord()
			if err := p.TerminalParser.Close(); err != nil {
				logger.Errorf("Session %s: close terminal parser failed: %s", p.id, err)
			}
			close(p.cmdRecordChan)
			close(p.userOutputChan)
			close(p.srvOutputChan)
			p.zmodemParser.Cleanup()
			logger.Infof("Session %s: Parser routine done", p.id)
		}()
		cmdRecordTicker := time.NewTicker(time.Minute)
		defer cmdRecordTicker.Stop()
		zmodemTicker := time.NewTicker(time.Second)
		defer zmodemTicker.Stop()
		lastActiveTime := time.Now()
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
				p.UpdateActiveUser(msg)
				if len(b) > 0 {
					b = p.ParseUserInput(b)
				}
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
			case <-p.zmodemPromptRedraw:
				if promptRedrawTimer == nil {
					promptRedrawTimer = time.NewTimer(zmodemPromptRedrawDelay)
				} else {
					if !promptRedrawTimer.Stop() {
						select {
						case <-promptRedrawTimer.C:
						default:
						}
					}
					promptRedrawTimer.Reset(zmodemPromptRedrawDelay)
				}
				promptRedrawTimerC = promptRedrawTimer.C
				continue
			case <-promptRedrawTimerC:
				promptRedrawTimerC = nil
				select {
				case <-p.closed:
					return
				case p.userOutputChan <- charEnter:
				}
				continue
			case now := <-cmdRecordTicker.C:
				// 每隔一分钟超时，尝试结算一次命令
				if now.Sub(lastActiveTime) > time.Minute {
					p.sendCommandRecord()
					p.TerminalParser.TryMultipleCommands()
				}
				continue
			case now := <-zmodemTicker.C:
<<<<<<< HEAD
=======
				if p.awaitingZmodemOO && !p.zmodemOOUntil.IsZero() && !now.Before(p.zmodemOOUntil) {
					logger.Infof("Session %s: Zmodem OO wait timeout, resume terminal input", p.id)
					p.finishZmodemOOWait()
				}
				if p.awaitingZmodemSrvOO && !p.zmodemSrvOOUntil.IsZero() && !now.Before(p.zmodemSrvOOUntil) {
					logger.Infof("Session %s: Zmodem server OO wait timeout, resume terminal input", p.id)
					p.finishZmodemServerOOWait()
				}
>>>>>>> origin/dev
				if p.drainingZmodemInput && !p.zmodemInputDrainUntil.IsZero() && !now.Before(p.zmodemInputDrainUntil) {
					logger.Infof("Session %s: Zmodem input drain timeout, resume terminal input", p.id)
					p.finishZmodemInputDrain()
				}
				if p.zmodemParser.IsExpired(now, zmodemIdleTimeout) ||
					p.zmodemParser.IsSendHandshakeExpired(now, zmodemSendHandshakeTimeout) {
					logger.Warnf("Session %s: Zmodem transfer idle timeout", p.id)
					// 先用 PTY 中断结束远端 rz/sz，再等待浏览器的 CAN 标记清空已排队的文件块。
					p.startZmodemInputDrain(now)
					select {
					case <-p.closed:
						return
					case p.userOutputChan <- []byte{zmodemInterruptCtrlC}:
					}
					if p.zmodemParser.Abort() {
						p.abortedFileTransfer = false
<<<<<<< HEAD
=======
						p.oversizedFileTransfer = false
>>>>>>> origin/dev
						p.zmodemRejectMessage = ""
					}
				}
				continue
			}
			lastActiveTime = time.Now()
		}
	}()
	return p.userOutputChan, p.srvOutputChan
}

func (p *Parser) isEnterKeyPress(b []byte) bool {
	if bytes.LastIndex(b, charEnter) == 0 {
		return true
	}
	if len(b) > 1 && bytes.HasSuffix(b, charLF) && isLinux(p.platform) {
		return true
	}
	// 多行命令，会有 \r 字符，此处也需要拦截
	if bytes.ContainsRune(b, '\r') {
		return true
	}
	switch p.protocolType {
	case srvconn.ProtocolMySQL,
		srvconn.ProtocolMariadb,
		srvconn.ProtocolPostgresql,
		srvconn.ProtocolClickHouse,
		srvconn.ProtocolOracle,
		srvconn.ProtocolSQLServer:
		// terminal 右键粘贴时，没有 \r 只有 \n
		if bytes.ContainsRune(b, '\n') && bytes.ContainsRune(b, ';') {
			return true
		}
	}
	return false
}

// parseInputState 切换用户输入状态, 并结算命令和结果
func (p *Parser) parseInputState(b []byte) []byte {
<<<<<<< HEAD
=======
	if p.awaitingZmodemSrvOO {
		// 下载结束阶段客户端回给远端的 ZFIN 必须原样通过，不能当作 shell 命令解析。
		return b
	}

	if p.awaitingZmodemOO {
		b = p.consumeZmodemOO(b)
		if len(b) == 0 {
			return nil
		}
	}

>>>>>>> origin/dev
	if p.drainingZmodemInput {
		// Ctrl-C 后浏览器会把 CAN 放在发送队列尾部；在此之前到达的内容都是残余文件数据。
		if bytes.Contains(b, zmodem.AbortSession) {
			p.finishZmodemInputDrain()
		}
		return nil
	}

	lang := i18n.NewLang(p.i18nLang)
	if p.zmodemParser.IsStartSession() {
		p.zmodemParser.MarkActive()
		userInterrupt := len(b) == 1 && b[0] == zmodemInterruptCtrlC
		if bytes.Contains(b, zmodem.AbortSession) || userInterrupt {
			logger.Infof("Session %s: user abort Zmodem transfer, control key: %t", p.id, userInterrupt)
			if userInterrupt {
				p.startZmodemInputDrain(time.Now())
			}
			rejected := p.abortedFileTransfer
			rejectMessage := p.zmodemRejectMessage
			status := p.zmodemParser.Status()
			p.abortedFileTransfer = false
<<<<<<< HEAD
=======
			p.oversizedFileTransfer = false
>>>>>>> origin/dev
			p.zmodemRejectMessage = ""
			p.zmodemParser.Abort()
			if rejected {
				msg := rejectMessage
				if msg == "" {
					msg = p.zmodemPermissionDeniedMessage(status)
				}
				p.srvOutputChan <- []byte("\r\n")
				p.srvOutputChan <- []byte(msg)
				p.srvOutputChan <- []byte("\r\n")
			}
			if !userInterrupt {
				return zmodem.CancelSequence
			}
			// rz/sz 会关闭 PTY 的信号处理，仅有 Ctrl-C 不一定退出；追加协议取消序列确保远端结束。
			cancelSequence := make([]byte, 0, 1+len(zmodem.CancelSequence))
			cancelSequence = append(cancelSequence, zmodemInterruptCtrlC)
			cancelSequence = append(cancelSequence, zmodem.CancelSequence...)
			return cancelSequence
		}

		switch p.zmodemParser.Status() {
		case zmodem.ZParserStatusReceive:
			p.zmodemParser.Parse(b)
			if p.zmodemParser.IsZFilePacket() {
<<<<<<< HEAD
=======
				if p.zmodemFileTooLarge() {
					rejectMessage := p.zmodemOversizedMessage()
					logger.Infof("Reject oversized Zmodem upload: %s", rejectMessage)
					p.abortedFileTransfer = true
					p.oversizedFileTransfer = true
					p.zmodemRejectMessage = rejectMessage
					p.zmodemParser.SetAbortMark()
					// 让客户端按标准 ZSKIP/ZFIN 流程退出，FinalShell 收到主动 CAN 会关闭整个 SSH 会话。
					p.srvOutputChan <- zmodem.SkipSequence
					return p.zmodemRemoteCancelSequence()
				}
>>>>>>> origin/dev
				rejectMessage := p.zmodemFileRejectMessage(zmodem.ZParserStatusReceive)
				if rejectMessage == "" {
					break
				}
				logger.Infof("Reject Zmodem upload: %s", rejectMessage)
				p.abortedFileTransfer = true
<<<<<<< HEAD
=======
				p.oversizedFileTransfer = false
>>>>>>> origin/dev
				p.zmodemRejectMessage = rejectMessage
				// 不记录中断的文件
				p.zmodemParser.SetAbortMark()
				p.srvOutputChan <- zmodem.SkipSequence
				return zmodem.AbortSession
			}

			if !p.zmodemParser.IsStartSession() && p.abortedFileTransfer {
				/*
					使用 zskip 中断文件上传之后，user 会发送 zfin 表示结束.
					此时，因为 srv 端已经中断，则不应接受 zmodem 字符，可以发nil
				*/

				logger.Info("Zmodem abort upload file finished")
				msg := p.zmodemRejectMessage
<<<<<<< HEAD
				p.abortedFileTransfer = false
=======
				oversized := p.oversizedFileTransfer
				p.abortedFileTransfer = false
				p.oversizedFileTransfer = false
>>>>>>> origin/dev
				p.zmodemRejectMessage = ""
				if msg == "" {
					msg = p.zmodemPermissionDeniedMessage(zmodem.ZParserStatusReceive)
				}
<<<<<<< HEAD
				p.srvOutputChan <- zmodem.CancelSequence
				p.srvOutputChan <- []byte("\r\n")
				p.srvOutputChan <- []byte(msg)
				p.srvOutputChan <- []byte("\r\n")
=======
				if !oversized {
					p.srvOutputChan <- zmodem.CancelSequence
					p.srvOutputChan <- []byte("\r\n")
					p.srvOutputChan <- []byte(msg)
					p.srvOutputChan <- []byte("\r\n")
				} else {
					// ZSKIP 后 FinalShell 会发送 ZFIN；必须回 ZFIN，它才会发送 OO 并释放终端界面。
					p.startZmodemOOWait(time.Now(), msg)
					p.srvOutputChan <- zmodem.FinishSequence
				}
				if oversized {
					return nil
				}
>>>>>>> origin/dev
				return charEnter
			}
		case zmodem.ZParserStatusSend:
			if p.zmodemParser.IsZFilePacket() {
<<<<<<< HEAD
=======
				if p.zmodemFileTooLarge() {
					rejectMessage := p.zmodemOversizedMessage()
					logger.Infof("Reject oversized Zmodem download: %s", rejectMessage)
					p.abortedFileTransfer = true
					p.oversizedFileTransfer = true
					p.zmodemRejectMessage = rejectMessage
					p.zmodemParser.SetAbortMark()
					// 代替客户端向远端发送 ZSKIP，随后把远端 ZFIN 转给客户端正常结束会话。
					return zmodem.SkipSequence
				}
>>>>>>> origin/dev
				rejectMessage := p.zmodemFileRejectMessage(zmodem.ZParserStatusSend)
				if rejectMessage == "" {
					break
				}
				logger.Infof("Reject Zmodem download: %s", rejectMessage)
				p.abortedFileTransfer = true
<<<<<<< HEAD
=======
				p.oversizedFileTransfer = false
>>>>>>> origin/dev
				p.zmodemRejectMessage = rejectMessage
				p.userOutputChan <- zmodem.AbortSession
				// 不记录中断的文件
				p.zmodemParser.SetAbortMark()
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

	WarnWaitMsg := lang.T("The command you executed is risky and an alert notification will be sent to the administrator. Do you want to continue?[Y/N]")
	if p.confirmStatus.InQuery() && p.getCurrentCmdStatusLevel() == model.WarningLevel {
		switch strings.ToLower(string(b)) {
		case "y":
			p.confirmStatus.SetStatus(StatusNone)
			p.userOutputChan <- []byte("\r\n")
		case "n":
			p.confirmStatus.SetStatus(StatusNone)
			p.srvOutputChan <- []byte("\r\n")
			p.TerminalParser.resetCommand()
			p.command = ""
			return p.breakInputPacket()
		default:
			p.srvOutputChan <- []byte("\r\n" + WarnWaitMsg)
		}
		return nil
	}

	confirmWaitMsg := lang.T("The command '%s' requires review. Continue or not [Y/n]?")
	if p.confirmStatus.InQuery() {
		switch strings.ToLower(string(b)) {
		case "y":
			p.confirmStatus.SetStatus(StatusStart)
			p.confirmStatus.wg.Add(1)
			go func() {
				p.confirmStatus.SetAction(model.ActionUnknown)
				p.waitCommandConfirm()
				defer p.confirmStatus.wg.Done()
				// 避免因为关闭chan造成的panic
				select {
				case <-p.closed:
					return
				default:
				}
				processor := p.confirmStatus.GetProcessor()
				switch p.confirmStatus.GetAction() {
				case model.ActionAccept:
					p.setCurrentCmdStatusLevel(model.ReviewAccept)
					formatMsg := lang.T("%s approved")
					statusMsg := utils.WrapperString(fmt.Sprintf(formatMsg, processor), utils.Green)
					p.srvOutputChan <- []byte("\r\n")
					p.srvOutputChan <- []byte(statusMsg)
					p.userOutputChan <- []byte(p.confirmStatus.data)
				case model.ActionReject:
					p.setCurrentCmdStatusLevel(model.ReviewReject)
					formatMsg := lang.T("%s rejected")
					statusMsg := utils.WrapperString(fmt.Sprintf(formatMsg, processor), utils.Red)
					p.srvOutputChan <- []byte("\r\n")
					p.srvOutputChan <- []byte(statusMsg)
					p.forbiddenCommand(p.confirmStatus.Cmd)
				default:
					// 默认是取消 不执行
					p.setCurrentCmdStatusLevel(model.ReviewCancel)
					p.srvOutputChan <- []byte("\r\n")
					p.userOutputChan <- p.breakInputPacket()
				}
				// 审核结束, 重置状态
				p.confirmStatus.SetStatus(StatusNone)
			}()
		case "n":
			p.setCurrentCmdStatusLevel(model.ReviewCancel)
			p.confirmStatus.SetStatus(StatusNone)
			p.srvOutputChan <- []byte("\r\n")
			return p.breakInputPacket()
		default:
			confirmMsg := fmt.Sprintf(confirmWaitMsg, stripNewLine(p.confirmStatus.Cmd))
			p.srvOutputChan <- []byte("\r\n" + confirmMsg)
		}
		return nil
	}
	if currentCmd, ok1 := p.TerminalParser.WriteInput(b); ok1 {
		p.sendCommandRecord()
		p.command = currentCmd
		p.cmdCreateDate = time.Now()
<<<<<<< HEAD
		if decision, ok := p.consumeTerminalAIGrant(currentCmd); ok {
			p.applyTerminalAIGrant(decision)
		} else if rule, cmd, ok := p.IsMatchCommandRule(currentCmd); ok {
=======
		if rule, cmd, ok := p.IsMatchCommandRule(currentCmd); ok {
			logger.Infof("command_rule_matched session_id=%q command=%q matched_command=%q acl_id=%q acl_name=%q rule_id=%q rule_name=%q action=%q",
				p.id, currentCmd, cmd, rule.Acl.ID, rule.Acl.Name, rule.Item.ID, rule.Item.Name, rule.Acl.Action)
>>>>>>> origin/dev
			switch rule.Acl.Action {
			case model.ActionReject:
				p.setCurrentCmdStatusLevel(model.RejectLevel)
				p.setCurrentCmdFilterRule(rule)
				p.forbiddenCommand(cmd)
				return nil
			case model.ActionReview:
				p.setCurrentCmdFilterRule(rule)
				p.confirmStatus.SetStatus(StatusQuery)
				p.confirmStatus.SetRule(rule)
				p.confirmStatus.SetCmd(p.command)
				p.confirmStatus.SetData(string(b))
				p.confirmStatus.ResetCtx()
				confirmMsg := fmt.Sprintf(confirmWaitMsg, stripNewLine(p.confirmStatus.Cmd))
				p.srvOutputChan <- []byte("\r\n" + confirmMsg)
				return nil
			case model.ActionWarning:
				p.setCurrentCmdFilterRule(rule)
				p.setCurrentCmdStatusLevel(model.WarningLevel)
			case model.ActionNotifyAndWarn:
				p.confirmStatus.SetStatus(StatusQuery)
				p.setCurrentCmdFilterRule(rule)
				p.setCurrentCmdStatusLevel(model.WarningLevel)
				p.srvOutputChan <- []byte("\r\n" + WarnWaitMsg)
				return nil
			default:
			}
		}
		if strings.Contains(p.command, "\r") {
			// 先记录一次 多行命令的输入，output 暂且为空
			p.sendCommandToChan()
			p.command = ""
		}
	}
	return b
}

<<<<<<< HEAD
func (p *Parser) consumeTerminalAIGrant(command string) (CommandACLDecision, bool) {
	if p.terminalAIGrant == nil {
		return CommandACLDecision{}, false
	}
	return p.terminalAIGrant(command)
}

func (p *Parser) applyTerminalAIGrant(decision CommandACLDecision) {
	for index := range p.cmdFilterACLs {
		rule := &p.cmdFilterACLs[index]
		if rule.ID != decision.ACLID {
			continue
		}
		for itemIndex := range rule.CommandGroups {
			item := &rule.CommandGroups[itemIndex]
			if item.ID == decision.ItemID {
				p.setCurrentCmdFilterRule(CommandRule{Acl: rule, Item: item})
				break
			}
		}
		break
	}
	if decision.Reviewed {
		p.setCurrentCmdStatusLevel(model.ReviewAccept)
		return
	}
	switch decision.Action {
	case model.ActionWarning, model.ActionNotifyAndWarn:
		p.setCurrentCmdStatusLevel(model.WarningLevel)
	}
}

=======
>>>>>>> origin/dev
func (p *Parser) zmodemPermissionDeniedMessage(status string) string {
	lang := i18n.NewLang(p.i18nLang)
	if status == zmodem.ZParserStatusSend {
		return lang.T("have no permission to download file")
	}
	return lang.T("have no permission to upload file")
}

func (p *Parser) zmodemFileRejectMessage(status string) string {
	if status == zmodem.ZParserStatusSend {
		if !p.enableDownload {
			return p.zmodemPermissionDeniedMessage(status)
		}
	} else if !p.enableUpload {
		return p.zmodemPermissionDeniedMessage(status)
	}

<<<<<<< HEAD
	info := p.zmodemParser.GetCurrentZFileInfo()
	if info == nil || info.Size() < zmodemMaxTransferSize {
		return ""
	}
=======
	if !p.zmodemFileTooLarge() {
		return ""
	}
	return p.zmodemOversizedMessage()
}

func (p *Parser) zmodemFileTooLarge() bool {
	info := p.zmodemParser.GetCurrentZFileInfo()
	return info != nil && info.Size() >= zmodemMaxTransferSize
}

func (p *Parser) zmodemOversizedMessage() string {
>>>>>>> origin/dev
	lang := i18n.NewLang(p.i18nLang)
	return fmt.Sprintf(lang.T("File exceeds maximum transfer size: %s"), zmodemMaxTransferSizeLabel)
}

<<<<<<< HEAD
=======
func (p *Parser) zmodemRemoteCancelSequence() []byte {
	cancelSequence := make([]byte, 0, 1+len(zmodem.CancelSequence))
	cancelSequence = append(cancelSequence, zmodemInterruptCtrlC)
	cancelSequence = append(cancelSequence, zmodem.CancelSequence...)
	return cancelSequence
}

func (p *Parser) startZmodemOOWait(now time.Time, notice string) {
	p.awaitingZmodemOO = true
	p.zmodemOOSeen = 0
	p.zmodemOOUntil = now.Add(zmodemFinishTimeout)
	p.zmodemOONotice = notice
}

func (p *Parser) resetZmodemOOWait() {
	p.awaitingZmodemOO = false
	p.zmodemOOSeen = 0
	p.zmodemOOUntil = time.Time{}
	p.zmodemOONotice = ""
}

func (p *Parser) finishZmodemOOWait() {
	notice := p.zmodemOONotice
	p.resetZmodemOOWait()
	if notice != "" {
		p.srvOutputChan <- []byte("\r\n")
		p.srvOutputChan <- []byte(notice)
		p.srvOutputChan <- []byte("\r\n")
	}
	p.scheduleZmodemPromptRedraw()
}

func (p *Parser) consumeZmodemOO(b []byte) []byte {
	for index, value := range b {
		if value == 'O' {
			p.zmodemOOSeen++
			if p.zmodemOOSeen == 2 {
				p.finishZmodemOOWait()
				return b[index+1:]
			}
			continue
		}
		p.zmodemOOSeen = 0
	}
	return nil
}

func (p *Parser) startZmodemServerOOWait(now time.Time, notice string) {
	p.awaitingZmodemSrvOO = true
	p.zmodemSrvOOSeen = 0
	p.zmodemSrvOOUntil = now.Add(zmodemFinishTimeout)
	p.zmodemSrvOONotice = notice
}

func (p *Parser) resetZmodemServerOOWait() {
	p.awaitingZmodemSrvOO = false
	p.zmodemSrvOOSeen = 0
	p.zmodemSrvOOUntil = time.Time{}
	p.zmodemSrvOONotice = ""
}

func (p *Parser) finishZmodemServerOOWait() {
	notice := p.zmodemSrvOONotice
	p.resetZmodemServerOOWait()
	if notice != "" {
		p.srvOutputChan <- []byte("\r\n")
		p.srvOutputChan <- []byte(notice)
		p.srvOutputChan <- []byte("\r\n")
	}
	p.scheduleZmodemPromptRedraw()
}

func (p *Parser) consumeZmodemServerOO(b []byte) []byte {
	for index, value := range b {
		if value == 'O' {
			p.zmodemSrvOOSeen++
			if p.zmodemSrvOOSeen == 2 {
				p.finishZmodemServerOOWait()
				return b[index+1:]
			}
			continue
		}
		p.zmodemSrvOOSeen = 0
	}
	return nil
}

func (p *Parser) rejectOversizedZmodemDownloadOffer() bool {
	if p.zmodemParser.Status() != zmodem.ZParserStatusSend ||
		!p.zmodemParser.IsZFilePacket() || !p.zmodemFileTooLarge() {
		return false
	}

	rejectMessage := p.zmodemOversizedMessage()
	logger.Infof("Reject oversized Zmodem download offer: %s", rejectMessage)
	p.abortedFileTransfer = true
	p.oversizedFileTransfer = true
	p.zmodemRejectMessage = rejectMessage
	p.zmodemParser.SetAbortMark()
	// 不把超限 ZFILE 发给 FinalShell，直接代替客户端向远端发送 ZSKIP。
	p.userOutputChan <- zmodem.SkipSequence
	return true
}

>>>>>>> origin/dev
func (p *Parser) startZmodemInputDrain(now time.Time) {
	p.drainingZmodemInput = true
	p.zmodemInputDrainUntil = now.Add(zmodemInputDrainTimeout)
}

func (p *Parser) finishZmodemInputDrain() {
	p.drainingZmodemInput = false
	p.zmodemInputDrainUntil = time.Time{}
<<<<<<< HEAD
=======
	p.scheduleZmodemPromptRedraw()
}

func (p *Parser) scheduleZmodemPromptRedraw() {
>>>>>>> origin/dev
	// 给远端 rz/sz 留出退出时间，再补回车让 shell 单独重绘提示符。
	select {
	case p.zmodemPromptRedraw <- struct{}{}:
	default:
	}
}

func (p *Parser) supportMultiCmd() bool {
	switch p.protocolType {
	case model.ProtocolSSH,
		model.ProtocolTelnet,
		model.ProtocolK8S:
		return true
	default:
		return false
	}
}

func (p *Parser) IsNeedParse() bool {
	p.modeLock.RLock()
	defer p.modeLock.RUnlock()
	if p.inVimState {
		return false
	}
	return true
}

func (p *Parser) forbiddenCommand(cmd string) {
	lang := i18n.NewLang(p.i18nLang)
	fbdMsg := fmt.Sprintf(lang.T("Command `%s` is forbidden"), cmd)
	p.srvOutputChan <- []byte("\r\n" + utils.WrapperWarn(fbdMsg))
	p.output = fbdMsg
	p.sendCommandToChan()
	p.TerminalParser.resetCommand()
	p.userOutputChan <- p.breakInputPacket()
}

// ParseUserInput 解析用户的输入
func (p *Parser) ParseUserInput(b []byte) []byte {
	if p.userInputFilter != nil {
		b = p.userInputFilter(b)
	}
	nb := p.parseInputState(b)
	return nb
}

const maxZmodemStartFrameSize = 64

// filterZmodemStart 返回可以安全送入终端解析器的数据。可能被拆包的 rz/sz
// 启动帧会被短暂缓存；原始数据仍由 splitCmdStream 原样转发给终端用户。
func (p *Parser) filterZmodemStart(b []byte) []byte {
	data := b
	if p.zmodemStartBuf.Len() > 0 {
		p.zmodemStartBuf.Write(b)
		data = bytes.Clone(p.zmodemStartBuf.Bytes())
		p.zmodemStartBuf.Reset()
	}

	prefix := zmodem.HexHeaderPrefix
	start := bytes.Index(data, prefix)
	if start < 0 {
		maxPrefix := min(len(data), len(prefix)-1)
		for size := maxPrefix; size > 0; size-- {
			if bytes.Equal(data[len(data)-size:], prefix[:size]) {
				p.zmodemStartBuf.Write(data[len(data)-size:])
				return data[:len(data)-size]
			}
		}
		return data
	}

	header := data[start:]
	if bytes.IndexAny(header, "\r\n") < 0 {
		if len(header) <= maxZmodemStartFrameSize {
			p.zmodemStartBuf.Write(header)
			return data[:start]
		}
		return data
	}

	p.zmodemParser.Parse(header)
	if p.zmodemParser.IsStartSession() {
		return data[:start]
	}
	return data
}

// updateTerminalMode combines the VT alternate-screen state with the command
// that entered it. Vim-like programs update the screen but suspend command
// parsing; tmux/screen continue normal command parsing inside the multiplexer.
func (p *Parser) updateTerminalMode(b []byte) {
	alternate := p.TerminalParser.IsScreenAlternate()
	p.modeLock.Lock()
	defer p.modeLock.Unlock()
	if !alternate {
		p.isEditMode = false
		p.inVimState = false
		p.isScreenMode = false
		return
	}

	p.isEditMode = true
	if IsEditExitMode(b) && p.inVimState {
		// Exiting an editor nested inside tmux restores the tmux alternate
		// screen, so alternate may remain true here.
		p.inVimState = false
		return
	}
	if IsEditEnterMode(b) && isTerminalEditorCommand(p.command) {
		p.inVimState = true
		return
	}
	if isTerminalMultiplexerCommand(p.command) || isNewScreen(b) {
		p.isScreenMode = true
		p.inVimState = false
		return
	}
	if !p.isScreenMode && !p.inVimState && matchMark(b, vimMarks) {
		// Compatibility fallback for editors launched by shell wrappers that
		// hide the executable name. It is only evaluated after VT confirms that
		// the alternate screen is active.
		p.inVimState = true
	}
}

func terminalCommandName(command string) string {
	fields := strings.Fields(command)
	for len(fields) > 0 {
		field := strings.Trim(fields[0], "'\"")
		fields = fields[1:]
		if strings.Contains(field, "=") || field == "sudo" || field == "env" ||
			field == "command" || field == "exec" {
			continue
		}
		if index := strings.LastIndexByte(field, '/'); index >= 0 {
			field = field[index+1:]
		}
		return field
	}
	return ""
}

func isTerminalMultiplexerCommand(command string) bool {
	switch terminalCommandName(command) {
	case "tmux", "screen":
		return true
	default:
		return false
	}
}

func isTerminalEditorCommand(command string) bool {
	switch terminalCommandName(command) {
	case "vi", "view", "vim", "vimdiff", "nvim", "nano", "emacs",
		"less", "more", "man", "top", "htop":
		return true
	default:
		return false
	}
}

func (p *Parser) consumeTerminalOutput(b []byte) {
	if len(b) == 0 {
		return
	}
	p.TerminalParser.FeedScreen(b)
	p.updateTerminalMode(b)
	if p.IsNeedParse() {
		p.TerminalParser.ProcessOutput(b)
	}
}

// splitCmdStream 将服务器输出流分离到命令buffer和命令输出buffer
func (p *Parser) splitCmdStream(b []byte) []byte {
<<<<<<< HEAD
	original := b
=======
	if p.awaitingZmodemSrvOO {
		b = p.consumeZmodemServerOO(b)
		if len(b) == 0 {
			return nil
		}
	}

>>>>>>> origin/dev
	lang := i18n.NewLang(p.i18nLang)
	if p.zmodemParser.IsStartSession() {
		p.zmodemParser.MarkActive()
		if p.zmodemParser.Status() == zmodem.ZParserStatusSend {
			p.zmodemParser.Parse(b)
			if p.rejectOversizedZmodemDownloadOffer() {
				return nil
			}
		}
		if p.zmodemParser.ConsumeLastAbort() {
			b = sanitizeZmodemAbortOutput(b)
		}
		if p.zmodemParser.ConsumeLastAbort() {
			b = sanitizeZmodemAbortOutput(b)
		}
		if !p.zmodemParser.IsStartSession() && p.abortedFileTransfer {
			logger.Info("Zmodem abort download file finished")
			msg := p.zmodemRejectMessage
<<<<<<< HEAD
			p.abortedFileTransfer = false
=======
			oversized := p.oversizedFileTransfer
			p.abortedFileTransfer = false
			p.oversizedFileTransfer = false
>>>>>>> origin/dev
			p.zmodemRejectMessage = ""
			p.srvOutputChan <- b
			if msg == "" {
				msg = lang.T("have no permission to download file")
			}
<<<<<<< HEAD
=======
			if oversized {
				p.startZmodemServerOOWait(time.Now(), msg)
				return nil
			}
>>>>>>> origin/dev
			p.srvOutputChan <- []byte("\r\n")
			p.srvOutputChan <- []byte(msg)
			p.srvOutputChan <- []byte("\r\n")
			p.userOutputChan <- charEnter
			return nil
		}
		return b
	} else {
		// Editor repaint data may contain arbitrary control bytes. Continue to
		// update TerminalVT, but don't interpret it as zmodem or command output.
		if !p.IsNeedParse() {
			p.consumeTerminalOutput(b)
			return original
		}
		parseBytes := p.filterZmodemStart(b)
		if p.zmodemParser.IsStartSession() {
			p.consumeTerminalOutput(parseBytes)
			logger.Infof("Zmodem start session %s", p.zmodemParser.Status())
			return original
		}
		b = parseBytes
		if p.zmodemParser.ConsumeLastAbort() {
			b = sanitizeZmodemAbortOutput(b)
			p.consumeTerminalOutput(b)
			return b
		}
<<<<<<< HEAD
=======
		p.parseZmodemState(b)
		if p.rejectOversizedZmodemDownloadOffer() {
			return nil
		}
		if p.zmodemParser.ConsumeLastAbort() {
			return sanitizeZmodemAbortOutput(b)
		}
>>>>>>> origin/dev
	}
	p.consumeTerminalOutput(b)
	return original
}

// sanitizeZmodemAbortOutput removes protocol frames while preserving remote errors and the shell prompt.
func sanitizeZmodemAbortOutput(b []byte) []byte {
	const (
		can = byte(0x18)
		bs  = byte(0x08)
	)

	cleaned := make([]byte, 0, len(b))
	remaining := b
	for len(remaining) > 0 {
		index := bytes.Index(remaining, zmodem.HexHeaderPrefix)
		if index == -1 {
			cleaned = append(cleaned, remaining...)
			break
		}

		prefix := remaining[:index]
		if bytes.HasSuffix(prefix, []byte("rz\r")) {
			prefix = prefix[:len(prefix)-len("rz\r")]
		}
		cleaned = append(cleaned, prefix...)

		header := remaining[index:]
		end := bytes.IndexByte(header, 0x8a)
		if end == -1 {
			end = bytes.IndexByte(header, '\n')
		}
		if end == -1 {
			break
		}
		end++
		if end < len(header) && header[end] == 0x11 {
			end++
		}
		remaining = header[end:]
	}

	result := cleaned[:0]
	for index := 0; index < len(cleaned); {
		if cleaned[index] != can {
			result = append(result, cleaned[index])
			index++
			continue
		}

		end := index
		for end < len(cleaned) && cleaned[end] == can {
			end++
		}
		if end-index < len(zmodem.AbortSession) {
			result = append(result, cleaned[index:end]...)
			index = end
			continue
		}
		for end < len(cleaned) && cleaned[end] == bs {
			end++
		}
		index = end
	}
	return result
}

// sanitizeZmodemAbortOutput removes protocol frames while preserving remote errors and the shell prompt.
func sanitizeZmodemAbortOutput(b []byte) []byte {
	const (
		can = byte(0x18)
		bs  = byte(0x08)
	)

	cleaned := make([]byte, 0, len(b))
	remaining := b
	for len(remaining) > 0 {
		index := bytes.Index(remaining, zmodem.HexHeaderPrefix)
		if index == -1 {
			cleaned = append(cleaned, remaining...)
			break
		}

		prefix := remaining[:index]
		if bytes.HasSuffix(prefix, []byte("rz\r")) {
			prefix = prefix[:len(prefix)-len("rz\r")]
		}
		cleaned = append(cleaned, prefix...)

		header := remaining[index:]
		end := bytes.IndexByte(header, 0x8a)
		if end == -1 {
			end = bytes.IndexByte(header, '\n')
		}
		if end == -1 {
			break
		}
		end++
		if end < len(header) && header[end] == 0x11 {
			end++
		}
		remaining = header[end:]
	}

	result := cleaned[:0]
	for index := 0; index < len(cleaned); {
		if cleaned[index] != can {
			result = append(result, cleaned[index])
			index++
			continue
		}

		end := index
		for end < len(cleaned) && cleaned[end] == can {
			end++
		}
		if end-index < len(zmodem.AbortSession) {
			result = append(result, cleaned[index:end]...)
			index = end
			continue
		}
		for end < len(cleaned) && cleaned[end] == bs {
			end++
		}
		index = end
	}
	return result
}

// ParseServerOutput 解析服务器输出
func (p *Parser) ParseServerOutput(b []byte) []byte {
	p.outputLock.Lock()
	defer p.outputLock.Unlock()
	return p.splitCmdStream(b)
}

// IsMatchCommandRule 判断命令是不是在过滤规则中
func (p *Parser) IsMatchCommandRule(command string) (CommandRule,
	string, bool) {
	for i := range p.cmdFilterACLs {
		rule := p.cmdFilterACLs[i]
		item, allowed, cmd := rule.Match(command)
		switch allowed {
		case model.ActionAccept, model.ActionWarning, model.ActionNotifyAndWarn:
			return CommandRule{Acl: &rule, Item: &item}, cmd, true
		case model.ActionReview, model.ActionReject:
			return CommandRule{Acl: &rule, Item: &item}, cmd, true
		default:
		}
	}
	return CommandRule{}, "", false
}

type CommandRule struct {
	Acl  *model.CommandACL
	Item *model.CommandFilterItem
}

func (p *Parser) waitCommandConfirm() {
	cmd := p.confirmStatus.Cmd
	rule := p.confirmStatus.Rule
	resp, err := p.jmsService.SubmitCommandReview(p.id, rule.Acl.ID, p.confirmStatus.Cmd)
	if err != nil {
		logger.Errorf("Session %s: submit command confirm api err: %s", p.id, err)
		p.confirmStatus.SetAction(model.ActionReject)
		return
	}
	lang := i18n.NewLang(p.i18nLang)
	checkReq := resp.CheckReq
	cancelReq := resp.CloseReq
	detailURL := resp.TicketDetailUrl
	reviewers := resp.Reviewers
	msg := lang.T("Please waiting for the reviewers to confirm command `%s`, cancel by CTRL+C or CTRL+D.")
	cmd = strings.ReplaceAll(cmd, "\r", "")
	cmd = strings.ReplaceAll(cmd, "\n", "")
	waitMsg := fmt.Sprintf(msg, cmd)
	checkTimer := time.NewTicker(10 * time.Second)
	defer checkTimer.Stop()
	ctx, cancelFunc := context.WithCancel(p.confirmStatus.ctx)
	defer cancelFunc()
	go func() {
		delay := 0
		titleMsg := lang.T("Need ticket confirm to execute command, already send email to the reviewers")
		reviewersMsg := fmt.Sprintf(lang.T("Ticket Reviewers: %s"), strings.Join(reviewers, ", "))
		detailURLMsg := fmt.Sprintf(lang.T("Could copy website URL to notify reviewers: %s"), detailURL)
		spinner := []string{".   ", "..  ", "... "}
		var tipString strings.Builder
		tipString.WriteString(utils.CharNewLine)
		tipString.WriteString(titleMsg)
		tipString.WriteString(utils.CharNewLine)
		tipString.WriteString(reviewersMsg)
		tipString.WriteString(utils.CharNewLine)
		tipString.WriteString(detailURLMsg)
		tipString.WriteString(utils.CharNewLine)
		tipString.WriteString(waitMsg)
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
				currentSpinner := spinner[delay%len(spinner)]
				data := strings.Repeat("\x08", len(delayS)+len(currentSpinner)) + currentSpinner + delayS
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
		switch statusResp.State {
		case model.TicketOpen:
			continue
		case model.TicketApproved:
			p.confirmStatus.SetAction(model.ActionAccept)
			p.confirmStatus.SetProcessor(statusResp.Processor)
			return
		case model.TicketRejected, model.TicketClosed:
			p.confirmStatus.SetProcessor(statusResp.Processor)
			p.confirmStatus.SetAction(model.ActionReject)
			return
		default:
			logger.Errorf("Receive unknown command confirm status %s", statusResp.Status)
		}
	}
}

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
	logger.Infof("Session %s: Parser close", p.id)
}

func (p *Parser) sendCommandRecord() {
	if p.command != "" {
		p.output = p.TerminalParser.TryOutput()
		p.sendCommandToChan()
		return
	}

}

func (p *Parser) EmitCommandEvent(cmd string, outputBuf string) {
	if cmd == "" {
		logger.Debugf("Session %s: Command cannot be empty: %s", p.id, outputBuf)
		return
	}
	p.command = cmd
	p.output = outputBuf
	p.sendCommandToChan()
}

func (p *Parser) sendCommandToChan() {
	if p.command == "" {
		return
	}
	cmd := p.command
	output := p.output
	cmdFilterId := ""
	cmdGroupId := ""
	if rule := p.getCurrentCmdFilterRule(); rule.Acl != nil {
		cmdFilterId = rule.Acl.ID
		cmdGroupId = rule.Item.ID
	}
	p.cmdRecordChan <- &ExecutedCommand{
		Command:        cmd,
		Output:         output,
		CreatedDate:    p.cmdCreateDate,
		RiskLevel:      p.getCurrentCmdStatusLevel(),
		CmdFilterACLId: cmdFilterId,
		CmdGroupId:     cmdGroupId,
		User:           p.currentActiveUser,
	}
	p.setCurrentCmdStatusLevel(model.NormalLevel)
	p.resetCurrentCmdFilterRule()
	p.command = ""
	p.output = ""
}

func (p *Parser) NeedRecord() bool {
	return !p.IsInZmodemRecvState()
}

func (p *Parser) CommandRecordChan() chan *ExecutedCommand {
	return p.cmdRecordChan
}

func (p *Parser) UpdateActiveUser(msg *exchange.RoomMessage) {
	p.currentActiveUser.UserId = msg.Meta.UserId
	p.currentActiveUser.User = msg.Meta.User
}

type ExecutedCommand struct {
	Command     string
	Output      string
	CreatedDate time.Time
	RiskLevel   int64
	User        CurrentActiveUser

	CmdFilterACLId string
	CmdGroupId     string
}

type CurrentActiveUser struct {
	UserId     string
	User       string
	RemoteAddr string
}

func isNewScreen(p []byte) bool {
	return matchMark(p, screenMarks)
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

/*

 h3c 的 ssh 拦截

 华为 telnet ssh

*/

const (
	h3c     = "h3c"
	huawei  = "huawei"
	cisco   = "cisco"
	linux   = "linux"
	windows = "windows"

	mfaAuth = "mfa"
)

func isH3C(p *model.Platform) bool {
	return isPlatform(p, h3c)
}

func isHuaWei(p *model.Platform) bool {
	return isPlatform(p, huawei)
}

func isCisco(p *model.Platform) bool {
	return isPlatform(p, cisco)
}

func isLinux(p *model.Platform) bool {
	return isPlatform(p, linux)
}

func isWindows(p *model.Platform) bool {
	return isPlatform(p, windows)
}

func isPlatform(p *model.Platform, platform string) bool {
	name := strings.ToLower(p.Name)
	os := strings.ToLower(p.BaseOs)
	ok := strings.Contains(name, platform) || strings.Contains(os, platform)
	return ok
}

func (p *Parser) breakInputPacket() []byte {
	switch p.protocolType {
	case model.ProtocolTelnet:
		if isHuaWei(p.platform) {
			return []byte{CharCTRLE, utils.CharCleanLine, '\r'}
		}
		if isCisco(p.platform) || isLinux(p.platform) {
			return []byte{CharCTRLE, utils.CharCleanLine, '\r'}
		}
		if isH3C(p.platform) {
			return []byte{CharCTRLE, CharCTRLX, '\r'}
		}
		return []byte{tclientlib.IAC, tclientlib.BRK, CharCTRLC, '\r'}
	case model.ProtocolSSH:
		if isH3C(p.platform) {
			return []byte{CharCTRLE, CharCTRLX, '\r'}
		}
		return []byte{CharCTRLE, utils.CharCleanLine, '\r'}
	default:
	}
	return []byte{CharCTRLE, utils.CharCleanLine, '\r'}
}

/*
	Ctrl + U --> 清除光标左边字符 '\x15'
	Ctrl + K --> 清除光标右边字符 '\x0B'
	Ctrl + E --> 移动光标到行末尾 '\x05'
*/

const (
	CharCleanRightLine = '\x0B'
	CharCTRLC          = '\x03'
	CharCTRLE          = '\x05'
	CharCTRLX          = '\x18'
)
