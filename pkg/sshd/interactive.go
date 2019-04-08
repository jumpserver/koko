package sshd

import (
	"cocogo/pkg/asset"
	"cocogo/pkg/core"
	"context"
	"fmt"
	"io"
	"runtime"
	"strings"

	"github.com/gliderlabs/ssh"
	gossh "golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/terminal"
)

const welcomeTemplate = `
		{{.UserName}}	Welcome to use Jumpserver open source fortress system
{{.Tab}}1) Enter {{.ColorCode}}ID{{.ColorEnd}} directly login or enter {{.ColorCode}}part IP, Hostname, Comment{{.ColorEnd}} to search login(if unique). {{.EndLine}}
{{.Tab}}2) Enter {{.ColorCode}}/{{.ColorEnd}} + {{.ColorCode}}IP, Hostname{{.ColorEnd}} or {{.ColorCode}}Comment{{.ColorEnd}} search, such as: /ip. {{.EndLine}}
{{.Tab}}3) Enter {{.ColorCode}}p{{.ColorEnd}} to display the host you have permission.{{.EndLine}}
{{.Tab}}4) Enter {{.ColorCode}}g{{.ColorEnd}} to display the node that you have permission.{{.EndLine}}
{{.Tab}}5) Enter {{.ColorCode}}g{{.ColorEnd}} + {{.ColorCode}}NodeID{{.ColorEnd}} to display the host under the node, such as g1. {{.EndLine}}
{{.Tab}}6) Enter {{.ColorCode}}s{{.ColorEnd}} Chinese-english switch.{{.EndLine}}
{{.Tab}}7) Enter {{.ColorCode}}h{{.ColorEnd}} help.{{.EndLine}}
{{.Tab}}8) Enter {{.ColorCode}}r{{.ColorEnd}} to refresh your assets and nodes.{{.EndLine}}
{{.Tab}}0) Enter {{.ColorCode}}q{{.ColorEnd}} exit.{{.EndLine}}
`

type HelpInfo struct {
	UserName  string
	ColorCode string
	ColorEnd  string
	Tab       string
	EndLine   string
}

func (d HelpInfo) displayHelpInfo(sess ssh.Session) {
	e := displayTemplate.Execute(sess, d)
	if e != nil {
		log.Warn("display help info failed")
	}
}

func InteractiveHandler(sess ssh.Session) {
	_, winCh, ptyOk := sess.Pty()
	if ptyOk {

		helpInfo := HelpInfo{
			UserName:  sess.User(),
			ColorCode: "\033[32m",
			ColorEnd:  "\033[0m",
			Tab:       "\t",
			EndLine:   "\r\n\r",
		}

		log.Info("accept one session")
		helpInfo.displayHelpInfo(sess)
		term := terminal.NewTerminal(sess, "Opt>")

		for {
			fmt.Println("start g num:", runtime.NumGoroutine())
			ctx, cancelFuc := context.WithCancel(sess.Context())
			go func() {
				for {
					select {
					case <-ctx.Done():
						fmt.Println("ctx done")
						return
					case win, ok := <-winCh:
						if !ok {
							return
						}
						fmt.Println("InteractiveHandler term change:", win)
						_ = term.SetSize(win.Width, win.Height)
					}
				}
			}()
			line, err := term.ReadLine()
			cancelFuc()
			if err != nil {
				log.Error("ReadLine done", err)
				break
			}
			switch line {
			case "p", "P":
				_, err := io.WriteString(sess, "p cmd execute\r\n")
				if err != nil {
					return
				}
			case "g", "G":
				_, err := io.WriteString(sess, "g cmd execute\r\n")
				if err != nil {
					return
				}
			case "s", "S":
				_, err := io.WriteString(sess, "s cmd execute\r\n")
				if err != nil {
					return
				}
			case "h", "H":
				helpInfo.displayHelpInfo(sess)

			case "r", "R":
				_, err := io.WriteString(sess, "r cmd execute\r\n")
				if err != nil {
					return
				}
			case "q", "Q", "exit", "quit":
				log.Info("exit session")
				return
			default:
				searchNodeAndProxy(line, sess)
				fmt.Println("end g num:", runtime.NumGoroutine())
			}
		}

	} else {
		_, err := io.WriteString(sess, "No PTY requested.\n")
		if err != nil {
			return
		}
	}

}

func searchNodeAndProxy(line string, sess ssh.Session) {
	searchWord := strings.TrimPrefix(line, "/")
	if strings.Contains(searchWord, "join") {

		roomID := strings.TrimSpace(strings.Join(strings.Split(searchWord, "join"), ""))
		sshConn := &SSHConn{
			conn: sess,
			uuid: generateNewUUID(),
		}
		log.Info("join room id: ", roomID)
		ctx, cancelFuc := context.WithCancel(context.Background())

		_, winCh, _ := sess.Pty()
		go func() {
			for {
				select {
				case <-ctx.Done():
					return
				case win, ok := <-winCh:
					if !ok {
						return
					}
					fmt.Println("join term change:", win)
				}
			}
		}()
		core.JoinShareRoom(roomID, sshConn)
		log.Info("exit room id:", roomID)
		cancelFuc()
		return

	}
	if node, ok := searchNode(searchWord); ok {
		err := MyProxy(sess, node)
		if err != nil {
			log.Info("proxy err ", err)
		}
	}
}

func searchNode(key string) (asset.Node, bool) {
	if key == "docker" {
		return asset.Node{
			IP:       "127.0.0.1",
			Port:     "32768",
			UserName: "root",
			PassWord: "screencast",
		}, true
	}
	return asset.Node{}, false
}

func MyProxy(userSess ssh.Session, node asset.Node) error {

	/*
		1. 创建SSHConn，符合core.Conn接口
		2. 创建一个session Home
		3. 创建一个NodeConn，及相关的channel 可以是MemoryChannel 或者是redisChannel
		4. session Home 与 proxy channel 交换数据
	*/
	sshConn := &SSHConn{
		conn: userSess,
		uuid: generateNewUUID(),
	}

	userHome := core.NewUserSessionHome(sshConn)
	log.Info("session room id:", userHome.SessionID())

	c, s, err := CreateNodeSession(node)
	if err != nil {
		return err
	}
	nodeC, err := core.NewNodeConn(c, s, sshConn)
	if err != nil {
		return err
	}
	mc := core.NewMemoryChannel(nodeC)
	err = core.Switch(sshConn.Context(), userHome, mc)
	return err

}

func CreateNodeSession(node asset.Node) (c *gossh.Client, s *gossh.Session, err error) {
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
