package main

import (
	"bytes"
	"fmt"
	"golang.org/x/crypto/ssh/terminal"
	"io"
	"io/ioutil"
	"os"
)

var f, _ = os.Create("/tmp/new.txt")

var buf, _ = ioutil.ReadFile("/tmp/cmd.text")

type CmdRwParser struct {
	content []byte
}

func (c *CmdRwParser) Read(b []byte) (int, error) {
	for i, v := range c.content {
		b[i] = v
	}
	fmt.Printf("Read %s\n", b)
	return len(c.content), io.EOF
}

func (c *CmdRwParser) Write(b []byte) (int, error) {
	fmt.Printf("Write %s\n", b)
	return len(b), nil
}

func main() {
	nb := new(bytes.Buffer)
	term := terminal.NewTerminal(nb, ">")
	nb.Write(buf)
	nb.Write([]byte("\r"))
	fmt.Printf("Buf: %s\n", buf)

	line, _ := term.ReadLine()
	f.WriteString(line)
	fmt.Printf("Line: %s\n", []byte(line))
	fmt.Println(".......................")
	fmt.Printf(nb.String())
	f.Close()
}
