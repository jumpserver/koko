package main

import (
	"cocogo/pkg/config"
	"cocogo/pkg/sshd"
)

func init() {
	config.Initial()
	sshd.Initial()
}

func main() {
	sshd.StartServer()
}
