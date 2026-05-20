package proxy

import (
	"errors"
	"io"
	"net"
	"strconv"
	"sync"

	"github.com/jumpserver-dev/sdk-go/model"
	"github.com/jumpserver/koko/pkg/logger"
	gossh "golang.org/x/crypto/ssh"
)

type domainGateway struct {
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
			logger.Errorf("Domain gateway %s accept conn err: %s", d.selectedGateway.Name, err)
			break
		}
		go d.handlerConn(con)
	}
	logger.Infof("Domain gateway %s stop listen on %s", d.selectedGateway.Name, d.ln.Addr())
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
	<-done
	logger.Infof("Gateway %s connect %s(%p) done", d.selectedGateway.Name, dstAddr, dstCon)
}

var ErrNoAvailable = errors.New("no available domain")

func (d *domainGateway) Start() (err error) {
	if !d.getAvailableGateway() {
		return ErrNoAvailable
	}
	d.ln, err = net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		_ = d.sshClient.Close()
		return err
	}
	go d.run()
	logger.Infof("Domain Gateway %s start listen on %s", d.selectedGateway.Name, d.ln.Addr())

	return nil
}
func (d *domainGateway) GetListenAddr() *net.TCPAddr {
	return d.ln.Addr().(*net.TCPAddr)
}

func (d *domainGateway) getAvailableGateway() bool {
	if d.selectedGateway != nil {
		sshClient, err := newGatewaySSHClient(d.selectedGateway)
		if err != nil {
			logger.Errorf("Dial select gateway %s err: %s ", d.selectedGateway.Name, err)
			return false
		}
		d.sshClient = sshClient
		return true
	}
	return false
}

func (d *domainGateway) Stop() {
	d.closeOnce()
}

func (d *domainGateway) closeOnce() {
	d.once.Do(func() {
		_ = d.ln.Close()
		_ = d.sshClient.Close()
		logger.Debugf("Domain Gateway %s close listen and gateway ssh client", d.selectedGateway.Name)
	})
}
