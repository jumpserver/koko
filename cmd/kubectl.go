package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/jumpserver/koko/pkg/config"
	"github.com/jumpserver/koko/pkg/utils"
)

const (
	commandName = "rawkubectl"
	envName     = "K8S_ENCRYPTED_TOKEN"
)

func main() {
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
		commandPrefix = fmt.Sprintf("%s --token=%s", commandName, token)
	}

	commandString := fmt.Sprintf("%s %s", commandPrefix, s.String())
	c := exec.Command("bash", "-c", commandString)
	c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
	_ = c.Run()
}
