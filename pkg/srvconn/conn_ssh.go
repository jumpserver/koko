package srvconn

import (
	"errors"
	"io"

	gossh "golang.org/x/crypto/ssh"
	"golang.org/x/text/transform"

	"github.com/jumpserver/koko/pkg/common"
)

func NewSSHConnection(sess *gossh.Session, opts ...SSHOption) (*SSHConnection, error) {
	if sess == nil {
		return nil, errors.New("ssh session is nil")
	}
	options := &SSHOptions{
		charset: common.UTF8,
		win: Windows{
			Width:  80,
			Height: 120,
		},
		term: "xterm",
	}
	for _, setter := range opts {
		setter(options)
	}
	modes := gossh.TerminalModes{
		gossh.ECHO:          1,     // enable echoing
		gossh.TTY_OP_ISPEED: 14400, // input speed = 14.4 kbaud
		gossh.TTY_OP_OSPEED: 14400, // output speed = 14.4 kbaud
	}
	err := sess.RequestPty(options.term, options.win.Height, options.win.Width, modes)
	if err != nil {
		return nil, err
	}
	stdin, err := sess.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := sess.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if options.charset != common.UTF8 {
		if readDecode := common.LookupCharsetDecode(options.charset); readDecode != nil {
			stdout = transform.NewReader(stdout, readDecode)
		}
		if writerEncode := common.LookupCharsetEncode(options.charset); writerEncode != nil {
			stdin = transform.NewWriter(stdin, writerEncode)
		}
	}
	err = sess.Shell()
	if err != nil {
		return nil, err
	}
	return &SSHConnection{
		session: sess,
		stdin:   stdin,
		stdout:  stdout,
		options: options,
	}, nil
}

type SSHConnection struct {
	session *gossh.Session
	stdin   io.Writer
	stdout  io.Reader
	options *SSHOptions
}

func (sc *SSHConnection) SetWinSize(w, h int) error {
	return sc.session.WindowChange(h, w)
}

func (sc *SSHConnection) Read(p []byte) (n int, err error) {
	return sc.stdout.Read(p)
}

func (sc *SSHConnection) Write(p []byte) (n int, err error) {
	return sc.stdin.Write(p)
}

func (sc *SSHConnection) Close() (err error) {
	return sc.session.Close()
}

func (sc *SSHConnection) KeepAlive() error {
	_, err := sc.session.SendRequest("keepalive@openssh.com", false, nil)
	return err
}

type SSHOption func(*SSHOptions)

type SSHOptions struct {
	charset string
	win     Windows
	term    string
}

func SSHCharset(charset string) SSHOption {
	return func(opt *SSHOptions) {
		opt.charset = charset
	}
}

func SSHPtyWin(win Windows) SSHOption {
	return func(opt *SSHOptions) {
		opt.win = win
	}
}

func SSHTerm(termType string) SSHOption {
	return func(opt *SSHOptions) {
		opt.term = termType
	}
}
