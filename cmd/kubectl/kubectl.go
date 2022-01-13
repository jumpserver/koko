package main

import (
	"os"
	"os/exec"

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

	args := make([]string, 0, len(os.Args))
	originArgs := os.Args[1:]
	for i := range originArgs {
		args = append(args, originArgs[i])
	}
	if token != "" {
		args = append(args, []string{"--token", token}...)
	}
	c := exec.Command(commandName, args...)
	c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
	_ = c.Run()
}
