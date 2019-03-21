package sshd

import (
	"bytes"
	"cocogo/pkg/asset"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gliderlabs/ssh"
	uuid "github.com/satori/go.uuid"
	gossh "golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/terminal"
)

const (
	maxBufferSize = 1024 * 4
)

type NodeProxy struct {
	uuid string

	userSess   ssh.Session
	nodeSess   *gossh.Session
	nodeClient *gossh.Client

	started     bool // 记录开始
	inputStatus bool // 是否是用户的输入状态

	userInputBuf     *bytes.Buffer // 用户输入的数据
	nodeCmdInputBuf  *bytes.Buffer // node对用户输入的回写数据
	nodeCmdOutputBuf *bytes.Buffer // node对用户按下enter按键之后，返回的数据

	nodeResponseCmdInputBuf  []byte
	nodeResponseCmdOutputBuf []byte

	sendUserStream chan []byte

	sendNodeStream chan []byte

	sync.Mutex
	ctx        context.Context
	nodeClosed bool
	userClosed bool

	term *terminal.Terminal

	rulerFilters []RuleFilter

	specialCommands []SpecialRuler

	forbiddenSignal bool

	inSpecialStatus bool
}

func NewNodeProxy(nodec *gossh.Client, nodes *gossh.Session, uers ssh.Session) *NodeProxy {
	userInputBuf := new(bytes.Buffer)
	return &NodeProxy{
		uuid:             uuid.NewV4().String(),
		userSess:         uers,
		nodeSess:         nodes,
		nodeClient:       nodec,
		started:          false,
		inputStatus:      false,
		ctx:              context.Background(),
		nodeClosed:       false,
		userClosed:       false,
		userInputBuf:     userInputBuf,
		nodeCmdInputBuf:  new(bytes.Buffer),
		nodeCmdOutputBuf: new(bytes.Buffer),
		sendUserStream:   make(chan []byte),
		sendNodeStream:   make(chan []byte),
		specialCommands:  []SpecialRuler{},
		term:             terminal.NewTerminal(userInputBuf, ""),
	}
}

func (n *NodeProxy) receiveNodeResponse(wg *sync.WaitGroup) {
	defer wg.Done()

	nodeStdout, err := n.nodeSess.StdoutPipe()
	if err != nil {
		log.Info(err)
		return
	}
	readBuf := make([]byte, maxBufferSize)

	for {
		nr, err := nodeStdout.Read(readBuf)
		if err != nil {
			break
		}
		if nr > 0 {

			/*
				是否是特殊命令状态：
					直接放过；

				是否是命令输入：
					是：
						放入nodeCmdInputBuf
					否：
						放入nodeCmdOutputBuf
			*/

			//开始输入之后，才开始记录输入的内容
			if n.started {
				// 对返回值进行解析，是否进入了特殊命令状态
				n.SpecialCommandFilter(readBuf[:nr])

				switch {
				case n.InSpecialCommandStatus():
					// 进入特殊命令状态，

				case n.forbiddenSignal:
					// 阻断命令的返回值

				case n.inputStatus:
					n.nodeCmdInputBuf.Write(readBuf[:nr])
				default:
					n.nodeCmdOutputBuf.Write(readBuf[:nr])
				}
			}

			n.sendUserStream <- readBuf[:nr]
		}

	}
	n.nodeClosed = true
	close(n.sendUserStream)

}

func (n *NodeProxy) sendUserResponse(wg *sync.WaitGroup) {
	defer wg.Done()
	for resBytes := range n.sendUserStream {
		nw, err := n.userSess.Write(resBytes)
		if nw != len(resBytes) || err != nil {
			break
		}
	}

}

func (n *NodeProxy) receiveUserRequest(wg *sync.WaitGroup) {

	defer wg.Done()

	readBuf := make([]byte, 1024)
	once := sync.Once{}
	path := filepath.Join("log", n.uuid)
	cmdRecord, _ := os.Create(path)

	defer cmdRecord.Close()

	var currentCommandInput string
	var currentCommandResult string

	for {
		nr, err := n.userSess.Read(readBuf)

		once.Do(func() {
			n.started = true
		})
		if err != nil {
			break
		}

		if n.nodeClosed {
			break
		}

		if nr > 0 {

			// 当inputStatus 为false
			/*
				enter之后
					是否需要解析
						是：
							解析用户真实执行的命令
								过滤命令：
								1、阻断则发送阻断msg 向node发送清除命令 和换行
						否:
				    		直接放过

			*/

			switch {
			case n.InSpecialCommandStatus():
				// vim 或者 rz 等状态

			case isEnterKey(readBuf[:nr]):

				currentCommandInput = n.ParseCommandInput()
				if currentCommandInput != "" && n.FilterCommand(currentCommandInput) {

					log.Info("cmd forbidden------>", currentCommandInput)
					msg := fmt.Sprintf("\r\n cmd '%s' is forbidden \r\n", currentCommandInput)
					n.sendUserStream <- []byte(msg)
					ctrU := []byte{21, 13} // 清除所有的输入
					n.inputStatus = true
					n.sendNodeStream <- ctrU
					n.forbiddenSignal = true

					data := CommandData{
						Input:     currentCommandInput,
						Output:    string(msg),
						Timestamp: time.Now().UTC().UnixNano(),
					}
					b, _ := json.Marshal(data)
					log.Info("write json data to file.")
					cmdRecord.Write(b)
					cmdRecord.Write([]byte("\r\n"))
					currentCommandInput = ""
					currentCommandResult = ""
					n.resetNodeInputOutBuf()
					continue
				}
				n.nodeCmdInputBuf.Reset()
				n.inputStatus = false
			default:

				fmt.Println(readBuf[:nr])
				if len(n.nodeCmdOutputBuf.Bytes()) > 0 && currentCommandInput != "" {
					log.Info("write cmd and result")
					currentCommandResult = n.ParseCommandResult()
					data := CommandData{
						Input:     currentCommandInput,
						Output:    currentCommandResult,
						Timestamp: time.Now().UTC().UnixNano(),
					}
					b, _ := json.Marshal(data)
					log.Info("write json data to file.")
					cmdRecord.Write(b)
					n.resetNodeInputOutBuf()
					currentCommandInput = ""
					currentCommandResult = ""
				}

				n.inputStatus = true
				n.forbiddenSignal = false
			}
			//解析命令且过滤命令

			n.sendNodeStream <- readBuf[:nr]
		}

	}
	close(n.sendNodeStream)

	n.nodeSess.Close()

	log.Info("receiveUserRequest exit---->")
}

func (n *NodeProxy) sendNodeRequest(wg *sync.WaitGroup) {

	defer wg.Done()

	nodeStdin, err := n.nodeSess.StdinPipe()
	if err != nil {
		return
	}

	for reqBytes := range n.sendNodeStream {
		nw, err := nodeStdin.Write(reqBytes)
		if nw != len(reqBytes) || err != nil {
			n.nodeClosed = true
			break
		}

	}

	log.Info("sendNodeStream closed")

}

// 匹配特殊命令,
func (n *NodeProxy) SpecialCommandFilter(b []byte) {
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

func (n *NodeProxy) MatchedSpecialCommand() (SpecialRuler, bool) {

	return nil, false
}

func (n *NodeProxy) InSpecialCommandStatus() bool {
	return n.inSpecialStatus
}

// 解析命令
func (n *NodeProxy) ParseCommandInput() string {
	// 解析用户输入的命令
	return n.nodeCmdInputBuf.String()
}

// 解析命令结果
func (n *NodeProxy) ParseCommandResult() string {
	return n.nodeCmdOutputBuf.String()
}

// 过滤所有的规则，判断是否阻止命令；如果是空字符串直接返回false
func (n *NodeProxy) FilterCommand(cmd string) bool {
	if strings.TrimSpace(cmd) == "" {
		return false
	}
	n.Lock()
	defer n.Unlock()
	for _, rule := range n.rulerFilters {
		if rule.Match(cmd) {
			log.Info("match rule", rule)
			return rule.BlockCommand()
		}
	}
	return false
}

func (n *NodeProxy) replayFileName() string {
	return fmt.Sprintf("%s.replay", n.uuid)
}

// 加载该资产的过滤规则
func (n *NodeProxy) LoadRuleFilters() {
	r1 := &Rule{
		priority: 10,
		action:   actionDeny,
		contents: []string{"ls"},
		ruleType: "command",
	}
	r2 := &Rule{
		priority: 10,
		action:   actionDeny,
		contents: []string{"pwd"},
		ruleType: "command",
	}
	n.Lock()
	defer n.Unlock()
	n.rulerFilters = []RuleFilter{r1, r2}
}

func (n *NodeProxy) resetNodeInputOutBuf() {
	n.nodeCmdInputBuf.Reset()
	n.nodeCmdOutputBuf.Reset()
}

func (n *NodeProxy) Start() error {
	var (
		err error
		wg  sync.WaitGroup
	)

	winChangeDone := make(chan struct{})

	ptyreq, winCh, _ := n.userSess.Pty()
	err = n.nodeSess.RequestPty(ptyreq.Term, ptyreq.Window.Height, ptyreq.Window.Width, gossh.TerminalModes{})
	if err != nil {
		return err
	}

	wg.Add(5)

	go func() {
		defer wg.Done()
		for {
			select {
			case <-winChangeDone:
				return
			case win := <-winCh:
				err = n.nodeSess.WindowChange(win.Height, win.Width)
				if err != nil {
					return
				}
				log.Info("windowChange: ", win)
			}
		}

	}()

	go n.receiveUserRequest(&wg)

	go n.sendNodeRequest(&wg)

	go n.receiveNodeResponse(&wg)

	go n.sendUserResponse(&wg)

	err = n.nodeSess.Shell()
	if err != nil {
		return err
	}
	err = n.nodeSess.Wait()

	winChangeDone <- struct{}{}
	wg.Wait()

	log.Info("wg done --->")

	if err != nil {
		return err
	}
	return nil

}

func CreateAssetNodeSession(node asset.Node) (c *gossh.Client, s *gossh.Session, err error) {
	config := &gossh.ClientConfig{
		User: node.UserName,
		Auth: []gossh.AuthMethod{
			gossh.Password(node.PassWord),
			gossh.PublicKeys(node.PublicKey),
		},
		HostKeyCallback: gossh.InsecureIgnoreHostKey(),
	}
	client, err := gossh.Dial("tcp", node.IP+":"+node.Port, config)
	if err != nil {
		log.Info(err)
		return c, s, err
	}
	s, err = client.NewSession()
	if err != nil {
		log.Error(err)
		return c, s, err
	}

	return client, s, nil
}

func Proxy(userSess ssh.Session, node asset.Node) error {
	nodeclient, nodeSess, err := CreateAssetNodeSession(node)
	if err != nil {
		return err
	}

	nproxy := NewNodeProxy(nodeclient, nodeSess, userSess)
	log.Info("session_uuid_id:", nproxy.uuid)

	nproxy.LoadRuleFilters()
	err = nproxy.Start()
	if err != nil {
		log.Error("nproxy err:", err)
		return err
	}

	fmt.Println("exit-------> Proxy")
	return nil
}

func isEnterKey(b []byte) bool {
	return b[0] == 13
}
