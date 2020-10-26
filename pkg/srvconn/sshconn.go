package srvconn

import (
	"errors"
	"io"

	gossh "golang.org/x/crypto/ssh"
	"golang.org/x/text/transform"

	"github.com/jumpserver/koko/pkg/model"
)

type ConnectionOption func(*connectionOptions)

type connectionOptions struct {
	charset string
}

func OptionCharset(charset string) ConnectionOption {
	return func(opt *connectionOptions) {
		opt.charset = charset
	}
}
func NewServerSSHConnection(sess *gossh.Session, opts ...ConnectionOption) *ServerSSHConnection {
	options := &connectionOptions{
		charset: model.UTF8,
	}
	for _, setter := range opts {
		setter(options)
	}
	return &ServerSSHConnection{
		session: sess,
		options: options,
	}
}

type ServerSSHConnection struct {
	session *gossh.Session
	stdin   io.Writer
	stdout  io.Reader
	options *connectionOptions
}

func (sc *ServerSSHConnection) Connect(h, w int, term string) (err error) {
	if sc.session == nil {
		return errors.New("ssh session is nil")
	}

	modes := gossh.TerminalModes{
		gossh.ECHO:          1,     // enable echoing
		gossh.TTY_OP_ISPEED: 14400, // input speed = 14.4 kbaud
		gossh.TTY_OP_OSPEED: 14400, // output speed = 14.4 kbaud
	}
	err = sc.session.RequestPty(term, h, w, modes)
	if err != nil {
		return
	}
	sc.stdin, err = sc.session.StdinPipe()
	if err != nil {
		return
	}
	sc.stdout, err = sc.session.StdoutPipe()
	if err != nil {
		return
	}
	if sc.options.charset != model.UTF8 {
		if readDecode := model.LookupCharsetDecode(sc.options.charset); readDecode != nil {
			sc.stdout = transform.NewReader(sc.stdout, readDecode)
		}
		if writerEncode := model.LookupCharsetEncode(sc.options.charset); writerEncode != nil {
			sc.stdin = transform.NewWriter(sc.stdin, writerEncode)
		}
	}
	return sc.session.Shell()
}

func (sc *ServerSSHConnection) SetWinSize(w, h int) error {
	return sc.session.WindowChange(h, w)
}

func (sc *ServerSSHConnection) Read(p []byte) (n int, err error) {
	return sc.stdout.Read(p)
}

func (sc *ServerSSHConnection) Write(p []byte) (n int, err error) {
	return sc.stdin.Write(p)
}

func (sc *ServerSSHConnection) Close() (err error) {
	return sc.session.Close()
}

func (sc *ServerSSHConnection) KeepAlive() error {
	_, err := sc.session.SendRequest("keepalive@openssh.com", true, nil)
	return err
}
