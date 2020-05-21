package srvconn

import (
	"io"
)

type ServerConnection interface {
	io.ReadWriteCloser
	Protocol() string
	SetWinSize(width, height int) error
}
