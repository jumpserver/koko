package main

import (
	"os"

	"github.com/jumpserver/koko/pkg/utils"
)

const (
	commandName = "rawhelm"
)

func main() {

	var  args []string

	token, _ := utils.GetDecryptedToken()
	if token != "" {
		args = append([]string{"--token", token}, os.Args[1:]...)
	}else{
		args = os.Args[1:]
	}

	utils.WrappedExec(commandName, args, token)
}