package proxy

import (
	"fmt"
	"net"
	"time"

	gossh "golang.org/x/crypto/ssh"
)

type ServerConnection interface {
	SendChannel() chan<- []byte
	RecvChannel() <-chan []byte
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
	proxyConn gossh.Conn
}

func (s *SSHConnection) Protocol() string {
	return "ssh"
}

func (s *SSHConnection) Config() (config *gossh.ClientConfig, err error) {
	auths := make([]gossh.AuthMethod, 0)
	if s.Password != "" {
		auths = append(auths, gossh.Password(s.Password))
	}
	if s.PrivateKeyPath != "" {
		if pubkey, err := GetPubKeyFromFile(s.PrivateKeyPath); err != nil {
			err = fmt.Errorf("parse private key from file error: %s", err)
			return config, err
		} else {
			auths = append(auths, gossh.PublicKeys(pubkey))
		}
	}
	if s.PrivateKey != "" {
		if signer, err := gossh.ParsePrivateKey([]byte(s.PrivateKey)); err != nil {
			err = fmt.Errorf("parse private key error: %s", err)
			return config, err
		} else {
			auths = append(auths, gossh.PublicKeys(signer))
		}
	}
	config = &gossh.ClientConfig{
		User:            s.User,
		Auth:            auths,
		HostKeyCallback: gossh.InsecureIgnoreHostKey(),
		Timeout:         s.Timeout,
	}
	return config, nil
}

func (s *SSHConnection) Connect() (client *gossh.Client, err error) {
	config, err := s.Config()
	if err != nil {
		return
	}
	if s.Proxy != nil {
		proxyClient, err := s.Proxy.Connect()
		if err != nil {
			return client, err
		}
		proxySock, err := proxyClient.Dial("tcp", net.JoinHostPort(s.Host, s.Port))
		if err != nil {
			return client, err
		}
		proxyConn, chans, reqs, err := gossh.NewClientConn(proxySock, net.JoinHostPort(s.Host, s.Port), config)
		if err != nil {
			return client, err
		}
		s.proxyConn = proxyConn
		client = gossh.NewClient(proxyConn, chans, reqs)
	} else {
		client, err = gossh.Dial("tcp", net.JoinHostPort(s.Host, s.Port), config)
		if err != nil {
			err = fmt.Errorf("connect host %s error: %s", s.Host, err)
			return
		}
	}
	s.client = client
	return client, nil
}

func (s *SSHConnection) Close() error {
	err := s.client.Close()
	if err != nil {
		return err
	}
	if s.proxyConn != nil {
		err = s.proxyConn.Close()
	}
	return err
}
