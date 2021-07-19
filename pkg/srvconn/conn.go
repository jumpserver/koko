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

	ProtocolMariadb = "mariadb"
)

var (
	ErrUnSupportedProtocol = errors.New("unsupported protocol")
)

var supportedMap = map[string]bool{
	ProtocolSSH:     true,
	ProtocolTELNET:  true,
	ProtocolK8s:     true,
	ProtocolMySQL:   true,
	ProtocolMariadb: true,
}

func IsSupportedProtocol(p string) bool {
	return supportedMap[p]
}
