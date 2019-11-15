package proxy

import (
	"bytes"
	"fmt"
	"github.com/jumpserver/koko/pkg/utils"
	"strings"
)

type commandInput struct {
	readFromUserInput   bytes.Buffer
	readFromServerInput bytes.Buffer

	isUserSideValid   bool
	isServerSideValid bool
}

func (c *commandInput) readFromUser(p []byte) {
	_, _ = c.readFromUserInput.Write(p)
}

func (c *commandInput) readFromServer(p []byte) {
	_, _ = c.readFromServerInput.Write(p)
}

func (c *commandInput) Parse() string {
	lines, ok := utils.ParseTerminalData(c.readFromUserInput.Bytes())

	if ok {
		fmt.Println("readFromUserInput lines: ", lines)
		c.readFromUserInput.Reset()
		c.readFromServerInput.Reset()
		return strings.Join(lines, "\r\n")
	}

	lines, _ = utils.ParseTerminalData(c.readFromServerInput.Bytes())
	fmt.Println("readFromServerInput lines: ", lines)
	c.readFromUserInput.Reset()
	c.readFromServerInput.Reset()
	return strings.Join(lines, "\r\n")
}

type commandOut struct {
	readFromServerOut bytes.Buffer
	isUserSideValid   bool
	isServerSideValid bool
}

func (c *commandOut) readFromServer(p []byte) {
	_, _ = c.readFromServerOut.Write(p)
}

func (c *commandOut) Parse() string {
	lines, _ := utils.ParseTerminalData(c.readFromServerOut.Bytes())
	c.readFromServerOut.Reset()
	fmt.Println("commandOut: ", lines)
	return strings.Join(lines, "\r\n")
}
