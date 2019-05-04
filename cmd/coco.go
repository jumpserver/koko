package main

import (
	"cocogo/pkg/config"
	"cocogo/pkg/service"
	"cocogo/pkg/sshd"
)

func init() {
	config.Initial()
}

func main() {
	service.Initial()
	sshd.StartServer()
}
