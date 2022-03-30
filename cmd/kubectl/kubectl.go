package main

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"strings"

	"github.com/jumpserver/koko/pkg/config"
	"github.com/jumpserver/koko/pkg/utils"
)

const (
	commandName = "rawkubectl"
	envName     = "K8S_ENCRYPTED_TOKEN"
)

func main() {
	gracefulStop := make(chan os.Signal, 1)
	// Ctrl + C 中断操作特殊处理，防止命令无法终止
	signal.Notify(gracefulStop, os.Interrupt)
	go func() {
		<-gracefulStop
		// 增加换行符
		fmt.Println("")
		os.Exit(1)
	}()

	encryptToken := os.Getenv(envName)
	var token string
	if encryptToken != "" {
		token, _ = utils.Decrypt(encryptToken, config.CipherKey)
	}

	args := os.Args[1:]
	var s strings.Builder
	for i := range args {
		s.WriteString(args[i])
		s.WriteString(" ")
	}
	commandPrefix := commandName
	if token != "" {
		token = strings.ReplaceAll(token, "'", "")
		commandPrefix = fmt.Sprintf(`%s --token='%s'`, commandName, token)
	}

	commandString := fmt.Sprintf("%s %s", commandPrefix, s.String())
	c := exec.Command("bash", "-c", commandString)
	c.Stdin, c.Stdout = os.Stdin, os.Stdout
	stderr, err := c.StderrPipe()
	if err != nil {
		log.Fatalln(err)
		return
	}
	redirectStream := func() {
		_, _ = io.Copy(os.Stderr, stderr)
	}
	if token != "" {
		redirectStream = func() {
			hiddenTokenOutput(stderr, os.Stderr, token)
		}
	}
	go redirectStream()
	_ = c.Run()
}

func hiddenTokenOutput(src io.ReadCloser, dst io.WriteCloser, token string) {
	tokenBuf := []byte(token)
	buf := make([]byte, 1024*8)
	var (
		index  int
		remain []byte
		buffer bytes.Buffer
	)
	for {
		nr, err2 := src.Read(buf)
		if nr > 0 {
			for i := range buf[:nr] {
				if index == len(tokenBuf) {
					index = 0
					remain = nil
					buffer.WriteString("*****")
					buffer.WriteByte(buf[i])
					continue
				}
				if buf[i] == tokenBuf[index] {
					index++
					remain = append(remain, buf[i])
					continue
				}
				if len(remain) > 0 {
					buffer.Write(remain)
					remain = nil
				}
				index = 0
				buffer.WriteByte(buf[i])
			}
			_, _ = buffer.WriteTo(dst)
			buffer.Reset()
		}
		if err2 != nil {
			return
		}
	}
}
