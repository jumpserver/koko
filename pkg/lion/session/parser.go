package session

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jumpserver/koko/pkg/lion/guacd"
	"github.com/jumpserver/koko/pkg/logger"

	"github.com/jumpserver-dev/sdk-go/model"
	"github.com/jumpserver-dev/sdk-go/service"
)

var (
	charEnter = []byte("\r")
)

var _ ParseEngine = (*Parser)(nil)

type Parser struct {
	id            string
	jmsService    *service.JMService
	cmdRecordChan chan *ExecutedCommand

	buf bytes.Buffer

	inputPreState bool
	inputState    bool
	once          *sync.Once
	lock          *sync.RWMutex

	command       string
	cmdCreateDate time.Time

	closed            chan struct{}
	currentActiveUser CurrentActiveUser
}

func (p *Parser) initial() {
	p.once = new(sync.Once)
	p.lock = new(sync.RWMutex)
	p.closed = make(chan struct{})
	p.cmdRecordChan = make(chan *ExecutedCommand, 1024)
}

// ParseStream 解析数据流
func (p *Parser) ParseStream(userInChan chan *Message) {
	logger.Infof("Session %s: Parser start", p.id)
	go func() {
		defer func() {
			p.ParseUserInput(charEnter)
			// 会话结束，结算命令结果
			p.sendCommandRecord()
			close(p.cmdRecordChan)
			logger.Infof("Session %s: Parser routine done", p.id)
		}()
		maxTimeout := time.Second * 20
		cmdRecordTicker := time.NewTicker(time.Second * 30)
		defer cmdRecordTicker.Stop()
		lastActiveTime := time.Now()
		for {
			select {
			case <-p.closed:
				return
			case now := <-cmdRecordTicker.C:
				if now.Sub(lastActiveTime) > maxTimeout {
					p.ParseUserInput(charEnter) //手动结算一次命令
				}
				continue
			case msg, ok := <-userInChan:
				if !ok {
					return
				}
				lastActiveTime = time.Now()
				p.UpdateActiveUser(msg)
				s := msg.Body
				var b []byte
				switch msg.Opcode {
				case guacd.InstructionMouse:
					var cmd string
					switch s[2] {
					case guacd.MouseLeft:
						cmd = "Left Button"
					case guacd.MouseRight:
						cmd = "Right Button"
					case guacd.MouseMiddle:
						cmd = "Middle Button"
					default:
						continue
					}
					p.ParseUserInput(charEnter) //手动结算一次命令
					cmd = fmt.Sprintf("Mouse Position[%s,%s] %s\r", s[0], s[1], cmd)
					b = append(b, []byte(cmd)...)
				case guacd.InstructionKey:
					switch s[1] {
					case guacd.KeyPress:
						keyCode, err := strconv.Atoi(s[0])
						if err == nil {
							cb := []byte(guacd.KeysymToCharacter(keyCode))
							if len(cb) == 0 {
								// guacamole-common.js unicode计算方法
								// if (codepoint >= 0x0100 && codepoint <= 0x10FFFF)
								//      return 0x01000000 | codepoint;
								if keyCode > 0x01000000 {
									var to string
									unicode := strconv.FormatInt(int64(keyCode), 16)
									bs, _ := hex.DecodeString(unicode[3:])
									for i, bl, br, r := 0, len(bs), bytes.NewReader(bs), uint16(0); i < bl; i += 2 {
										_ = binary.Read(br, binary.BigEndian, &r)
										to += string(rune(r))
									}
									b = append(b, []byte(to)...)
								} else {
									// 未知的键值,转成 rune 字符
									b = append(b, []byte(string(rune(keyCode)))...)
								}
							} else {
								b = append(b, cb...)
							}
						} else {
							b = append(b, []byte(guacd.KeyCodeUnknown)...)
						}
					default:
						continue
					}
				}
				if len(b) == 0 {
					continue
				}
				_, _ = p.WriteData(b)
				p.ParseUserInput(b)
			}
		}
	}()
}

// ParseUserInput 解析用户的输入
func (p *Parser) ParseUserInput(b []byte) {
	_ = p.parseInputState(b)
}

// parseInputState 切换用户输入状态, 并结算命令和结果
func (p *Parser) parseInputState(b []byte) []byte {
	p.inputPreState = p.inputState
	if bytes.LastIndex(b, charEnter) >= 0 {
		// 连续输入enter key, 结算上一条可能存在的命令结果
		p.sendCommandRecord()
		p.inputState = false
		// 用户输入了Enter，开始结算命令
		p.parseCmdInput()
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
func (p *Parser) parseCmdInput() {
	command := p.Parse()
	if len(command) <= 0 {
		p.command = ""
	} else {
		p.command = command
	}
	p.cmdCreateDate = time.Now()
}

func (p *Parser) WriteData(b []byte) (int, error) {
	p.lock.Lock()
	defer p.lock.Unlock()
	if p.buf.Len() >= 2048 {
		return 0, nil
	}
	if len(b) > 1 {
		p.buf.WriteByte(byte(' '))
	}
	return p.buf.Write(b)
}

func (p *Parser) Parse() string {
	line := p.buf.String()
	line = strings.TrimPrefix(line, string(charEnter))
	p.buf.Reset()
	return line
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
		p.cmdRecordChan <- &ExecutedCommand{
			Command:     p.command,
			CreatedDate: p.cmdCreateDate,
			RiskLevel:   model.NormalLevel,
			User:        p.currentActiveUser,
		}
		p.command = ""
	}
}

func (p *Parser) CommandRecordChan() chan *ExecutedCommand {
	return p.cmdRecordChan
}

func (p *Parser) UpdateActiveUser(msg *Message) {
	p.currentActiveUser.UserId = msg.Meta.UserId
	p.currentActiveUser.User = msg.Meta.User
}

type ExecutedCommand struct {
	Command     string
	Output      string
	CreatedDate time.Time
	RiskLevel   int
	User        CurrentActiveUser
}

type CurrentActiveUser struct {
	UserId string
	User   string
}
