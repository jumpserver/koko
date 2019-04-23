package main

import (
	"cocogo/pkg/config"
	"cocogo/pkg/sshd"
)

func init() {
	config.Initial()
}

func main() {
	sshd.StartServer()
}
