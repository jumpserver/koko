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
	Connect() error
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

func (sc *SSHConnection) Connect() (client *gossh.Client, err error) {
	config, err := sc.Config()
	if err != nil {
		return
	}
	if sc.Proxy != nil {
		proxyClient, err := sc.Proxy.Connect()
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
	sess, err := sc.client.NewSession()
	if err != nil {
		return
	}
	sc.Session = sess
	return client, nil
}

func (sc *SSHConnection) Reader() (reader io.Reader, err error) {
	return sc.Session.StdoutPipe()
}

func (sc *SSHConnection) Writer() (writer io.WriteCloser, err error) {
	return sc.Session.StdinPipe()
}

func (sc *SSHConnection) Close() error {
	err := sc.client.Close()
	if err != nil {
		return err
	}
	if sc.proxyConn != nil {
		err = sc.proxyConn.Close()
	}
	return err
}
