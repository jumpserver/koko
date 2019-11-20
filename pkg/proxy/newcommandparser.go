package proxy

import (
	"bytes"
	"fmt"
	"github.com/jumpserver/koko/pkg/utils"
	"strings"
)

type commandInput struct {
	readFromUserInput   *bytes.Buffer
	readFromServerInput *bytes.Buffer

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
	lines, ok := utils.ParseTerminalData([]byte(c.readFromUserInput.String()))

	if ok {
		fmt.Println("readFromUserInput lines: ", lines)
		c.readFromUserInput.Reset()
		c.readFromServerInput.Reset()
		result := strings.Join(lines, "\r\n")
		fmt.Println("readFromUserInput result: ", result, len(result))
		return result
	}

	lines, _ = utils.ParseTerminalData([]byte(c.readFromServerInput.String()))
	fmt.Println("readFromServerInput lines: ", lines)
	c.readFromUserInput.Reset()
	c.readFromServerInput.Reset()
	return strings.Join(lines, "\r\n")
}

type commandOut struct {
	readFromServerOut *bytes.Buffer
}

func (c *commandOut) readFromServer(p []byte) {
	_, _ = c.readFromServerOut.Write(p)
}

func (c *commandOut) Parse() string {
	lines, _ := utils.ParseTerminalData([]byte(c.readFromServerOut.String()))
	c.readFromServerOut.Reset()
	result := strings.Join(lines, "\r\n")
	fmt.Println("commandOut: ", result)
	return result
}


