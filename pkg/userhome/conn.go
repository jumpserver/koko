package userhome

import (
	"context"
	"io"

	"github.com/sirupsen/logrus"

	"github.com/gliderlabs/ssh"
	uuid "github.com/satori/go.uuid"
)

var log = logrus.New()

const maxBufferSize = 1024 * 4

type Conn interface {
	SessionID() string

	User() string

	UUID() uuid.UUID

	Pty() (ssh.Pty, <-chan ssh.Window, bool)

	Context() context.Context

	io.Reader
	io.WriteCloser
}
