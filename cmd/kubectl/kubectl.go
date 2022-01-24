package main

import (
	"fmt"
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
	c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
	_ = c.Run()
}
