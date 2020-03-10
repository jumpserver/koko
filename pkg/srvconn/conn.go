package srvconn

import (
	"io"
)

type ServerConnection interface {
	io.ReadWriteCloser
	Protocol() string
	SetWinSize(w, h int) error
}
