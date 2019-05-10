package proxy

import (
	"fmt"
	"io"
	"net"
	"time"

	gossh "golang.org/x/crypto/ssh"
)

type ServerConnection interface {
	io.ReadWriteCloser
	Name() string
	Host() string
	Port() string
	User() string
	Timeout() time.Duration
	Protocol() string
	Connect(h, w int, term string) error
	SetWinSize(w, h int) error
}

type ServerSSHConnection struct {
	name           string
	host           string
	port           string
	user           string
	password       string
	privateKey     string
	privateKeyPath string
	timeout        int
	Proxy          *ServerSSHConnection

	client    *gossh.Client
	Session   *gossh.Session
	proxyConn gossh.Conn
	stdin     io.WriteCloser
	stdout    io.Reader
	closed    bool
}

func (sc *ServerSSHConnection) Protocol() string {
	return "ssh"
}

func (sc *ServerSSHConnection) User() string {
	return sc.user
}

func (sc *ServerSSHConnection) Host() string {
	return sc.host
}

func (sc *ServerSSHConnection) Name() string {
	return sc.name
}

func (sc *ServerSSHConnection) Port() string {
	return sc.port
}

func (sc *ServerSSHConnection) Timeout() time.Duration {
	return time.Duration(sc.timeout) * time.Second
}

func (sc *ServerSSHConnection) String() string {
	return fmt.Sprintf("%s@%s:%s", sc.user, sc.host, sc.port)
}

func (sc *ServerSSHConnection) Config() (config *gossh.ClientConfig, err error) {
	authMethods := make([]gossh.AuthMethod, 0)
	if sc.password != "" {
		authMethods = append(authMethods, gossh.Password(sc.password))
	}
	if sc.privateKeyPath != "" {
		if pubkey, err := GetPubKeyFromFile(sc.privateKeyPath); err != nil {
			err = fmt.Errorf("parse private key from file error: %sc", err)
			return config, err
		} else {
			authMethods = append(authMethods, gossh.PublicKeys(pubkey))
		}
	}
	if sc.privateKey != "" {
		if signer, err := gossh.ParsePrivateKey([]byte(sc.privateKey)); err != nil {
			err = fmt.Errorf("parse private key error: %sc", err)
			return config, err
		} else {
			authMethods = append(authMethods, gossh.PublicKeys(signer))
		}
	}
	config = &gossh.ClientConfig{
		User:            sc.user,
		Auth:            authMethods,
		HostKeyCallback: gossh.InsecureIgnoreHostKey(),
		Timeout:         sc.Timeout(),
	}
	return config, nil
}

func (sc *ServerSSHConnection) connect() (client *gossh.Client, err error) {
	config, err := sc.Config()
	if err != nil {
		return
	}
	if sc.Proxy != nil {
		proxyClient, err := sc.Proxy.connect()
		if err != nil {
			return client, err
		}
		proxySock, err := proxyClient.Dial("tcp", net.JoinHostPort(sc.host, sc.port))
		if err != nil {
			return client, err
		}
		proxyConn, chans, reqs, err := gossh.NewClientConn(proxySock, net.JoinHostPort(sc.host, sc.port), config)
		if err != nil {
			return client, err
		}
		sc.proxyConn = proxyConn
		client = gossh.NewClient(proxyConn, chans, reqs)
	} else {
		client, err = gossh.Dial("tcp", net.JoinHostPort(sc.host, sc.port), config)
		if err != nil {
			err = fmt.Errorf("connect host %sc error: %sc", sc.host, err)
			return
		}
	}
	sc.client = client
	return client, nil
}

func (sc *ServerSSHConnection) invokeShell(h, w int, term string) (err error) {
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

func (sc *ServerSSHConnection) Connect(h, w int, term string) (err error) {
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

func (sc *ServerSSHConnection) SetWinSize(h, w int) error {
	return sc.Session.WindowChange(h, w)
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
	err = sc.Session.Close()
	err = sc.client.Close()
	if sc.proxyConn != nil {
		err = sc.proxyConn.Close()
	}
	sc.closed = true
	return
}
