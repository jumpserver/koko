package srvconn

import (
	"io"
	"time"
)

type ServerConnection interface {
	io.ReadWriteCloser
	Timeout() time.Duration
	Protocol() string
	SetWinSize(w, h int) error
}
