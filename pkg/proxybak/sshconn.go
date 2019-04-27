package proxybak

import (
	"cocogo/pkg/parser"
	"cocogo/pkg/record"
	"context"
	"fmt"
	"io"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/ibuler/ssh"

	gossh "golang.org/x/crypto/ssh"
)

var log = logrus.New()

const maxBufferSize = 1024 * 4

type ServerAuth struct {
	SessionID string
	IP        string
	Port      int
	UserName  string
	Password  string
	PublicKey gossh.Signer
}

type Conn interface {
	ReceiveRequest(context.Context, <-chan []byte, chan<- []byte)
	SendResponse(context.Context, chan<- []byte)
}

func CreateNodeSession(authInfo ServerAuth) (c *gossh.Client, s *gossh.Session, err error) {
	config := &gossh.ClientConfig{
		User: authInfo.UserName,
		Auth: []gossh.AuthMethod{
			gossh.Password(authInfo.Password),
			gossh.PublicKeys(authInfo.PublicKey),
		},
		HostKeyCallback: gossh.InsecureIgnoreHostKey(),
	}
	client, err := gossh.Dial("tcp", fmt.Sprintf("%s:%d", authInfo.IP, authInfo.Port), config)
	if err != nil {
		log.Error(err)
		return c, s, err
	}
	s, err = client.NewSession()
	if err != nil {
		log.Error(err)
		return c, s, err
	}

	return client, s, nil
}

func NewNodeConn(ctx context.Context, authInfo ServerAuth, ptyReq ssh.Pty, winCh <-chan ssh.Window) (*NodeConn, error) {
	c, s, err := CreateNodeSession(authInfo)
	if err != nil {
		return nil, err
	}

	err = s.RequestPty(ptyReq.Term, ptyReq.Window.Height, ptyReq.Window.Width, gossh.TerminalModes{})
	if err != nil {
		return nil, err
	}
	nodeStdin, err := s.StdinPipe()
	if err != nil {
		return nil, err
	}
	nodeStdout, err := s.StdoutPipe()
	if err != nil {
		return nil, err
	}

	err = s.Shell()
	if err != nil {
		return nil, err
	}
	subCtx, cancelFunc := context.WithCancel(ctx)

	replyRecord := record.NewReplyRecord(authInfo.SessionID)
	replyRecord.StartRecord()
	//go replyRecord.EndRecord(subCtx)
	nConn := &NodeConn{
		SessionID:     authInfo.SessionID,
		client:        c,
		conn:          s,
		ctx:           subCtx,
		ctxCancelFunc: cancelFunc,
		stdin:         nodeStdin,
		stdout:        nodeStdout,
		tParser:       parser.NewTerminalParser(),
		replyRecord:   replyRecord,
		StartTime:     time.Now().UTC(),
	}

	go nConn.windowChangeHandler(winCh)
	return nConn, nil
}

// coco连接远程Node的连接
type NodeConn struct {
	SessionID            string
	client               *gossh.Client
	conn                 *gossh.Session
	stdin                io.Writer
	stdout               io.Reader
	tParser              *parser.TerminalParser
	currentCommandInput  string
	currentCommandResult string
	rulerFilters         []parser.RuleFilter
	specialCommands      []parser.SpecialRuler
	inSpecialStatus      bool
	ctx                  context.Context
	ctxCancelFunc        context.CancelFunc
	replyRecord          *record.Reply
	cmdRecord            *record.Command
	StartTime            time.Time
}

func (n *NodeConn) Wait() error {
	return n.conn.Wait()
}

func (n *NodeConn) FilterSpecialCommand(b []byte) {
	for _, specialCommand := range n.specialCommands {
		if matched := specialCommand.MatchRule(b); matched {
			switch {
			case specialCommand.EnterStatus():
				n.inSpecialStatus = true
			case specialCommand.ExitStatus():
				n.inSpecialStatus = false

			}
		}
	}
}

func (n *NodeConn) FilterWhiteBlackRule(cmd string) bool {
	for _, rule := range n.rulerFilters {
		if rule.Match(cmd) {
			return rule.BlockCommand()
		}
	}
	return false
}

func (n *NodeConn) windowChangeHandler(winCH <-chan ssh.Window) {
	for {
		select {
		case <-n.ctx.Done():
			log.Info("windowChangeHandler done")
			return
		case win, ok := <-winCH:
			if !ok {
				return
			}
			err := n.conn.WindowChange(win.Height, win.Width)
			if err != nil {
				log.Error("windowChange err: ", win)
				return
			}
			log.Info("windowChange: ", win)
		}
	}

}

func (n *NodeConn) Close() {

	select {
	case <-n.ctx.Done():
		return
	default:
		_ = n.conn.Close()
		_ = n.client.Close()
		n.ctxCancelFunc()
		log.Info("Close conn")
	}
}

func (n *NodeConn) SendResponse(ctx context.Context, outChan chan<- []byte) {

	buf := make([]byte, maxBufferSize)
	defer close(outChan)
	for {
		nr, err := n.stdout.Read(buf)
		if err != nil {
			log.Error("read conn err:", err)
			return
		}

		if n.tParser.Started && nr > 0 {
			n.FilterSpecialCommand(buf[:nr])

			switch {
			case n.inSpecialStatus:
				// 进入特殊命令状态，
			case n.tParser.InputStatus:
				n.tParser.CmdInputBuf.Write(buf[:nr])
			case n.tParser.OutputStatus:
				n.tParser.CmdOutputBuf.Write(buf[:nr])
			default:

			}

		}

		select {
		case <-ctx.Done():
			log.Info("SendResponse finish by context done")
			return
		default:
			copyBuf := make([]byte, len(buf[:nr]))
			copy(copyBuf, buf[:nr])
			outChan <- copyBuf
			n.replyRecord.Record(buf[:nr])
		}
	}

}

func (n *NodeConn) ReceiveRequest(ctx context.Context, inChan <-chan []byte, outChan chan<- []byte) {
	defer n.Close()
	for {
		select {
		case <-ctx.Done():
			log.Error("ReceiveRequest finish by context  done")
			return
		case buf, ok := <-inChan:
			if !ok {
				log.Error("ReceiveRequest finish by inChan  close")
				return
			}

			n.tParser.Once.Do(
				func() {
					n.tParser.Started = true
				})

			switch {
			case n.inSpecialStatus:
				// 特殊的命令 vim 或者 rz

			case n.tParser.IsEnterKey(buf):
				n.currentCommandInput = n.tParser.ParseCommandInput()
				if n.FilterWhiteBlackRule(n.currentCommandInput) {
					msg := fmt.Sprintf("\r\n cmd '%s' is forbidden \r\n", n.currentCommandInput)
					outChan <- []byte(msg)
					n.replyRecord.Record([]byte(msg))
					ctrU := []byte{21, 13} // 清除行并换行
					_, err := n.stdin.Write(ctrU)
					if err != nil {
						log.Error(err)
					}
					n.tParser.InputStatus = false
					n.tParser.OutputStatus = false
					continue
				}
				n.tParser.InputStatus = false
				n.tParser.OutputStatus = true
			default:
				// 1. 是否是一个命令的完整周期 是则解析命令，记录结果 并重置
				// 2. 重置用户输入状态
				if len(n.tParser.CmdOutputBuf.Bytes()) > 0 && n.currentCommandInput != "" {
					n.currentCommandResult = n.tParser.ParseCommandResult()

					n.tParser.Reset()
					n.currentCommandInput = ""
					n.currentCommandResult = ""
				}
				n.tParser.InputStatus = true
			}

			_, err := n.stdin.Write(buf)
			if err != nil {
				log.Error("write conn err:", err)
				return
			}

		}
	}
}
