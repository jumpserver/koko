package proxy

import (
	"errors"
	"io"
	"net"
	"strconv"
	"sync"
	"time"

	gossh "golang.org/x/crypto/ssh"

	"github.com/jumpserver/koko/pkg/config"
	"github.com/jumpserver/koko/pkg/logger"
	"github.com/jumpserver/koko/pkg/model"
)

type domainGateway struct {
	domain  *model.Domain
	dstIP   string
	dstPort int

	sshClient       *gossh.Client
	selectedGateway *model.Gateway
	ln              net.Listener

	once sync.Once
}

func (d *domainGateway) run() {
	defer d.closeOnce()
	for {
		con, err := d.ln.Accept()
		if err != nil {
			logger.Errorf("Domain %s accept conn err: %s", d.domain.Name, err)
			break
		}
		go d.handlerConn(con)
	}
	logger.Infof("Domain %s stop listen on %s", d.domain.Name, d.ln.Addr())
}

func (d *domainGateway) handlerConn(srcCon net.Conn) {
	defer srcCon.Close()
	dstAddr := net.JoinHostPort(d.dstIP, strconv.Itoa(d.dstPort))
	dstCon, err := d.sshClient.Dial("tcp", dstAddr)
	if err != nil {
		logger.Errorf("Domain gateway connect %s err: %s", dstAddr, err)
		return
	}
	defer dstCon.Close()
	logger.Infof("Gateway %s connected %s(%p)", d.selectedGateway.Name, dstAddr, dstCon)
	done := make(chan struct{}, 2)
	go func() {
		_, _ = io.Copy(dstCon, srcCon)
		done <- struct{}{}
		logger.Debugf("Gateway %s dst %s(%p) stop write", d.selectedGateway.Name,
			dstAddr, dstCon)
	}()
	go func() {
		_, _ = io.Copy(srcCon, dstCon)
		done <- struct{}{}
		logger.Debugf("Gateway %s dst %s(%p) stop read", d.selectedGateway.Name,
			dstAddr, dstCon)
	}()
	select {
	case <-done:
	}
	logger.Infof("Gateway %s connect %s(%p) done", d.selectedGateway.Name, dstAddr, dstCon)
}

func (d *domainGateway) Start() (addr *net.TCPAddr, err error) {
	if !d.getAvailableGateway() {
		return nil, errors.New("no available domain")
	}
	d.ln, err = net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		_ = d.sshClient.Close()
		return nil, err
	}
	go d.run()
	logger.Infof("Domain %s start listen on %s", d.domain.Name, d.ln.Addr())

	return d.ln.Addr().(*net.TCPAddr), nil
}

func (d *domainGateway) getAvailableGateway() bool {
	configTimeout := config.GetConf().SSHTimeout
	for i := range d.domain.Gateways {
		gateway := d.domain.Gateways[i]
		if gateway.Protocol == "ssh" {
			auths := make([]gossh.AuthMethod, 0, 3)
			if gateway.Password != "" {
				auths = append(auths, gossh.Password(gateway.Password))
				auths = append(auths, gossh.KeyboardInteractive(func(user, instruction string,
					questions []string, echos []bool) (answers []string, err error) {
					return []string{gateway.Password}, nil
				}))
			}
			if gateway.PrivateKey != "" {
				if signer, err := gossh.ParsePrivateKey([]byte(gateway.PrivateKey)); err != nil {
					logger.Errorf("Domain gateway Parse private key error: %s", err)
				} else {
					auths = append(auths, gossh.PublicKeys(signer))
				}
			}
			sshConfig := gossh.ClientConfig{
				User:            gateway.Username,
				Auth:            auths,
				HostKeyCallback: gossh.InsecureIgnoreHostKey(),
				Timeout:         configTimeout * time.Second,
			}
			addr := net.JoinHostPort(gateway.IP, strconv.Itoa(gateway.Port))
			sshClient, err := gossh.Dial("tcp", addr, &sshConfig)
			logger.Debugf("Domain %s try dial gateway %s", d.domain.Name, gateway.Name)
			if err != nil {
				logger.Errorf("Dial gateway %s err: %s ", gateway.Name, err)
				continue
			}
			logger.Infof("Domain %s use gateway %s", d.domain.Name, gateway.Name)
			d.sshClient = sshClient
			d.selectedGateway = &gateway
			return true
		}
	}
	logger.Errorf("Domain %s has no available gateway", d.domain.Name)
	return false
}

func (d *domainGateway) Stop() {
	d.closeOnce()
}

func (d *domainGateway) closeOnce() {
	d.once.Do(func() {
		_ = d.ln.Close()
		_ = d.sshClient.Close()
		logger.Debugf("Domain %s close listen and gateway ssh client", d.domain.Name)
	})
}
