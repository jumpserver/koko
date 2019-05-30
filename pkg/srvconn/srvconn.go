package srvconn

import (
	"fmt"
	"github.com/pkg/errors"
	"io"
	"net"
	"time"

	gossh "golang.org/x/crypto/ssh"
)

type ServerConnection interface {
	io.ReadWriteCloser
	Timeout() time.Duration
	Protocol() string
	SetWinSize(w, h int) error
}

type SSHClientConfig struct {
	Host           string
	Port           string
	User           string
	Password       string
	PrivateKey     string
	PrivateKeyPath string
	Overtime       int
	Proxy          *SSHClientConfig

	proxyConn gossh.Conn
}

func (sc *SSHClientConfig) Timeout() time.Duration {
	if sc.Overtime == 0 {
		sc.Overtime = 30
	}
	return time.Duration(sc.Overtime) * time.Second
}

func (sc *SSHClientConfig) Config() (config *gossh.ClientConfig, err error) {
	authMethods := make([]gossh.AuthMethod, 0)
	if sc.Password != "" {
		authMethods = append(authMethods, gossh.Password(sc.Password))
	}
	if sc.PrivateKeyPath != "" {
		if pubkey, err := GetPubKeyFromFile(sc.PrivateKeyPath); err != nil {
			err = fmt.Errorf("parse private key from file error: %s", err)
			return config, err
		} else {
			authMethods = append(authMethods, gossh.PublicKeys(pubkey))
		}
	}
	if sc.PrivateKey != "" {
		if signer, err := gossh.ParsePrivateKey([]byte(sc.PrivateKey)); err != nil {
			err = fmt.Errorf("parse private key error: %s", err)
			return config, err
		} else {
			authMethods = append(authMethods, gossh.PublicKeys(signer))
		}
	}
	config = &gossh.ClientConfig{
		User:            sc.User,
		Auth:            authMethods,
		HostKeyCallback: gossh.InsecureIgnoreHostKey(),
		Timeout:         sc.Timeout(),
	}
	return config, nil
}

func (sc *SSHClientConfig) Dial() (client *gossh.Client, err error) {
	cfg, err := sc.Config()
	if err != nil {
		return
	}
	if sc.Proxy != nil && sc.Proxy.Host != "" {
		proxyClient, err := sc.Proxy.Dial()
		if err != nil {
			err = errors.New("connect proxy Host error1: " + err.Error())
			return client, err
		}
		proxySock, err := proxyClient.Dial("tcp", net.JoinHostPort(sc.Host, sc.Port))
		if err != nil {
			err = errors.New("connect proxy Host error2: " + err.Error())
			return client, err
		}
		proxyConn, chans, reqs, err := gossh.NewClientConn(proxySock, net.JoinHostPort(sc.Host, sc.Port), cfg)
		if err != nil {
			return client, err
		}
		sc.proxyConn = proxyConn
		client = gossh.NewClient(proxyConn, chans, reqs)
	} else {
		client, err = gossh.Dial("tcp", net.JoinHostPort(sc.Host, sc.Port), cfg)
		if err != nil {
			return
		}
	}
	return client, nil
}

func (sc *SSHClientConfig) String() string {
	return fmt.Sprintf("%s@%s:%s", sc.User, sc.Host, sc.Port)
}

type ServerSSHConnection struct {
	SSHClientConfig
	Name    string
	Creator string

	client   *gossh.Client
	session  *gossh.Session
	stdin    io.WriteCloser
	stdout   io.Reader
	closed   bool
	refCount int
}

func (sc *ServerSSHConnection) Protocol() string {
	return "ssh"
}

func (sc *ServerSSHConnection) String() string {
	return fmt.Sprintf("%s@%s:%s", sc.User, sc.Host, sc.Port)
}

func (sc *ServerSSHConnection) invokeShell(h, w int, term string) (err error) {
	sess, err := sc.client.NewSession()
	if err != nil {
		return
	}
	sc.session = sess
	modes := gossh.TerminalModes{
		gossh.ECHO:          1,     // enable echoing
		gossh.TTY_OP_ISPEED: 14400, // input speed = 14.4 kbaud
		gossh.TTY_OP_OSPEED: 14400, // output speed = 14.4 kbaud
	}
	err = sess.RequestPty(term, h, w, modes)
	if err != nil {
		return
	}
	sc.stdin, err = sess.StdinPipe()
	if err != nil {
		return
	}
	sc.stdout, err = sess.StdoutPipe()
	if err != nil {
		return
	}
	err = sess.Shell()
	return err
}

func (sc *ServerSSHConnection) Connect(h, w int, term string) (err error) {
	sc.client, err = sc.Dial()
	if err != nil {
		return
	}
	err = sc.invokeShell(h, w, term)
	if err != nil {
		return
	}
	return nil
}

func (sc *ServerSSHConnection) SetWinSize(h, w int) error {
	return sc.session.WindowChange(h, w)
}

func (sc *ServerSSHConnection) Read(p []byte) (n int, err error) {
	return sc.stdout.Read(p)
}

func (sc *ServerSSHConnection) Write(p []byte) (n int, err error) {
	return sc.stdin.Write(p)
}

func (sc *ServerSSHConnection) Close() (err error) {
	if sc.closed {
		return
	}
	err = sc.session.Close()
	err = sc.client.Close()
	if sc.proxyConn != nil {
		err = sc.proxyConn.Close()
	}
	sc.closed = true
	return
}
