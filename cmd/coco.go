package main

import (
	"cocogo/pkg/auth"
	"cocogo/pkg/config"
	"cocogo/pkg/sshd"
)

func init() {
	config.Initial()
	auth.Initial()
	sshd.Initial()
}

func main() {
	sshd.StartServer()
}
