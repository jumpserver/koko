package proxy

import (
	"fmt"
	"io"
	"net"
	"time"

	gossh "golang.org/x/crypto/ssh"
)

type ServerConnection interface {
	Writer() io.WriteCloser
	Reader() io.Reader
	Protocol() string
	Connect(h, w int, term string) error
	SetWinSize(w, h int) error
	Close()
}

type SSHConnection struct {
	Host           string
	Port           string
	User           string
	Password       string
	PrivateKey     string
	PrivateKeyPath string
	Timeout        time.Duration
	Proxy          *SSHConnection

	client    *gossh.Client
	Session   *gossh.Session
	proxyConn gossh.Conn
	stdin     io.WriteCloser
	stdout    io.Reader
	closed    bool
}

func (sc *SSHConnection) Protocol() string {
	return "ssh"
}

func (sc *SSHConnection) Config() (config *gossh.ClientConfig, err error) {
	auths := make([]gossh.AuthMethod, 0)
	if sc.Password != "" {
		auths = append(auths, gossh.Password(sc.Password))
	}
	if sc.PrivateKeyPath != "" {
		if pubkey, err := GetPubKeyFromFile(sc.PrivateKeyPath); err != nil {
			err = fmt.Errorf("parse private key from file error: %sc", err)
			return config, err
		} else {
			auths = append(auths, gossh.PublicKeys(pubkey))
		}
	}
	if sc.PrivateKey != "" {
		if signer, err := gossh.ParsePrivateKey([]byte(sc.PrivateKey)); err != nil {
			err = fmt.Errorf("parse private key error: %sc", err)
			return config, err
		} else {
			auths = append(auths, gossh.PublicKeys(signer))
		}
	}
	config = &gossh.ClientConfig{
		User:            sc.User,
		Auth:            auths,
		HostKeyCallback: gossh.InsecureIgnoreHostKey(),
		Timeout:         sc.Timeout,
	}
	return config, nil
}

func (sc *SSHConnection) connect() (client *gossh.Client, err error) {
	config, err := sc.Config()
	if err != nil {
		return
	}
	if sc.Proxy != nil {
		proxyClient, err := sc.Proxy.connect()
		if err != nil {
			return client, err
		}
		proxySock, err := proxyClient.Dial("tcp", net.JoinHostPort(sc.Host, sc.Port))
		if err != nil {
			return client, err
		}
		proxyConn, chans, reqs, err := gossh.NewClientConn(proxySock, net.JoinHostPort(sc.Host, sc.Port), config)
		if err != nil {
			return client, err
		}
		sc.proxyConn = proxyConn
		client = gossh.NewClient(proxyConn, chans, reqs)
	} else {
		client, err = gossh.Dial("tcp", net.JoinHostPort(sc.Host, sc.Port), config)
		if err != nil {
			err = fmt.Errorf("connect host %sc error: %sc", sc.Host, err)
			return
		}
	}
	sc.client = client
	return client, nil
}

func (sc *SSHConnection) invokeShell(h, w int, term string) (err error) {
	sess, err := sc.client.NewSession()
	if err != nil {
		return
	}
	sc.Session = sess
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

func (sc *SSHConnection) Connect(h, w int, term string) (err error) {
	_, err = sc.connect()
	if err != nil {
		return
	}
	err = sc.invokeShell(h, w, term)
	if err != nil {
		return
	}
	return nil
}

func (sc *SSHConnection) Reader() (reader io.Reader) {
	return sc.stdout
}

func (sc *SSHConnection) Writer() (writer io.WriteCloser) {
	return sc.stdin
}

func (sc *SSHConnection) SetWinSize(h, w int) error {
	return sc.Session.WindowChange(h, w)
}

func (sc *SSHConnection) Close() {
	if sc.closed {
		return
	}
	_ = sc.Session.Close()
	_ = sc.client.Close()
	if sc.proxyConn != nil {
		_ = sc.proxyConn.Close()
	}
	sc.closed = true
}
