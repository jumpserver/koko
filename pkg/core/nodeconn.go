package core

import (
	"context"
	"fmt"
	"io"

	uuid "github.com/satori/go.uuid"
	gossh "golang.org/x/crypto/ssh"
)

func NewNodeConn(c *gossh.Client, s *gossh.Session, useS Conn) (*NodeConn, error) {
	ptyReq, winCh, _ := useS.Pty()
	err := s.RequestPty(ptyReq.Term, ptyReq.Window.Height, ptyReq.Window.Width, gossh.TerminalModes{})
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
	ctx, cancel := context.WithCancel(useS.Context())
	Out, In := io.Pipe()
	go func() {
		for {
			select {
			case <-ctx.Done():
				log.Info("NewNodeConn goroutine closed")
				err = s.Close()
				err = c.Close()
				if err != io.EOF && err != nil {
					log.Info(" sess  Close():", err)
				}
				return
			case win, ok := <-winCh:
				if !ok {
					return
				}
				err = s.WindowChange(win.Height, win.Width)
				if err != nil {
					log.Info("windowChange err: ", win)
					return
				}
				log.Info("windowChange: ", win)
			}
		}
	}()

	go func() {
		nr, err := io.Copy(In, nodeStdout)
		if err != nil {
			log.Info("io copy err:", err)
		}

		err = In.Close()
		if err != nil {
			log.Info("io copy c.Close():", err)
		}
		cancel()

		log.Info("io copy int:", nr)
	}()
	nConn := &NodeConn{
		uuid:    uuid.NewV4(),
		client:  c,
		conn:    s,
		stdin:   nodeStdin,
		stdout:  nodeStdout,
		cusOut:  Out,
		cusIn:   In,
		tParser: NewTerminalParser(),
	}

	return nConn, nil
}

// coco连接远程Node的连接
type NodeConn struct {
	uuid                 uuid.UUID
	client               *gossh.Client
	conn                 *gossh.Session
	stdin                io.Writer
	stdout               io.Reader
	cusIn                io.WriteCloser
	cusOut               io.ReadCloser
	tParser              *TerminalParser
	currentCommandInput  string
	currentCommandResult string
	rulerFilters         []RuleFilter
	specialCommands      []SpecialRuler
	inSpecialStatus      bool
}

func (n *NodeConn) UUID() uuid.UUID {
	return n.uuid
}

func (n *NodeConn) Read(b []byte) (nr int, err error) {
	nr, err = n.cusOut.Read(b)

	if n.tParser.Started && nr > 0 {
		n.FilterSpecialCommand(b[:nr])

		switch {
		case n.inSpecialStatus:
			// 进入特殊命令状态，
		case n.tParser.InputStatus:
			n.tParser.CmdInputBuf.Write(b[:nr])
		case n.tParser.OutputStatus:
			n.tParser.CmdOutputBuf.Write(b[:nr])
		default:

		}

	}

	return nr, err
}

func (n *NodeConn) Write(b []byte) (nw int, err error) {
	n.tParser.Once.Do(
		func() {
			n.tParser.Started = true
		})

	switch {
	case n.inSpecialStatus:
		// 特殊的命令 vim 或者 rz

	case n.tParser.IsEnterKey(b):
		n.currentCommandInput = n.tParser.ParseCommandInput()
		if n.FilterWhiteBlackRule(n.currentCommandInput) {
			msg := fmt.Sprintf("\r\n cmd '%s' is forbidden \r\n", n.currentCommandInput)
			nw, err = n.cusIn.Write([]byte(msg))
			if err != nil {
				return nw, err
			}
			ctrU := []byte{21, 13} // 清除行并换行
			nw, err = n.stdin.Write(ctrU)
			if err != nil {
				return nw, err
			}
			n.tParser.InputStatus = false
			n.tParser.OutputStatus = false
			return len(b), nil
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
	return n.stdin.Write(b)
}

func (n *NodeConn) Close() error {
	return n.cusOut.Close()
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
