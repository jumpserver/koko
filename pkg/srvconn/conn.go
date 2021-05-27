package srvconn

import (
	"errors"
	"io"
)

type ServerConnection interface {
	io.ReadWriteCloser
	SetWinSize(width, height int) error
	KeepAlive() error
}

type Windows struct {
	Width  int
	Height int
}

const (
	ProtocolSSH    = "ssh"
	ProtocolTELNET = "telnet"
	ProtocolK8s    = "k8s"
	ProtocolMySQL  = "mysql"
)


var (
	ErrUnSupportedProtocol = errors.New("unsupported protocol")
)