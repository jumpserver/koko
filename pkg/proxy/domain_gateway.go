package proxy

import (
	"io"
	"net"
	"strconv"
	"sync"
	"time"

	"github.com/pkg/errors"
	gossh "golang.org/x/crypto/ssh"

	"github.com/jumpserver/koko/pkg/config"
	"github.com/jumpserver/koko/pkg/logger"
	"github.com/jumpserver/koko/pkg/model"
)

type domain struct {
	ln        net.Listener
	sshClient *gossh.Client

	dstAddr net.Addr

	gateways []model.Gateway

	done chan struct{}
	once sync.Once
}

func (d *domain) run() {
	for {
		con, err := d.ln.Accept()
		if err != nil {
			logger.Errorf("Domain listener err: %s", err)
			return
		}
		go d.handlerConn(con)
	}
}

func (d *domain) handlerConn(c net.Conn) {
	dstCon, err := d.sshClient.Dial(d.dstAddr.Network(), d.dstAddr.String())
	if err != nil {
		logger.Errorf("Domain gateway connect %s err: %s", d.dstAddr.String(), err)
		return
	}
	defer c.Close()
	defer dstCon.Close()

	go func() {
		_, _ = io.Copy(dstCon, c)
	}()
	_, _ = io.Copy(c, dstCon)
}

func (d *domain) Start() (addr net.Addr, err error) {
	if !d.getAvailableGateway() {
		return nil, errors.New("no available domain")
	}
	d.ln, err = net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		_ = d.sshClient.Close()
		return nil, err
	}
	go d.run()
	return d.ln.Addr(), nil
}

func (d *domain) getAvailableGateway() bool {
	configTimeout := config.GetConf().SSHTimeout
	for i := range d.gateways {
		gateway := d.gateways[i]
		if gateway.Protocol == "ssh" {
			auths := make([]gossh.AuthMethod, 0, 3)
			if d.gateways[i].Password != "" {
				auths = append(auths, gossh.Password(gateway.Password))
				auths = append(auths, gossh.KeyboardInteractive(func(user, instruction string,
					questions []string, echos []bool) (answers []string, err error) {
					return []string{gateway.Password}, nil
				}))
			}
			if gateway.PrivateKey != "" {
				if signer, err := gossh.ParsePrivateKeyWithPassphrase([]byte(gateway.PrivateKey),
					[]byte(gateway.Password)); err != nil {
					logger.Errorf("Domain gateway Parse private key error: %s", err)
				} else {
					auths = append(auths, gossh.PublicKeys(signer))
				}
			}

			sshConfig := gossh.ClientConfig{
				User:    d.gateways[i].Username,
				Auth:    auths,
				Timeout: configTimeout * time.Second,
			}
			addr := net.JoinHostPort(gateway.IP, strconv.Itoa(gateway.Port))
			sshClient, err := gossh.Dial("tcp", addr, &sshConfig)
			if err != nil {
				logger.Errorf("Domain dial gateway err: %s", err)
				continue
			}
			d.sshClient = sshClient
			return true
		}
	}
	return false
}
