package gateway

import (
	"errors"
	"io"
	"net"
	"strconv"
	"sync"
	"time"

	gossh "golang.org/x/crypto/ssh"

	"github.com/jumpserver/koko/pkg/logger"

	"github.com/jumpserver-dev/sdk-go/common"
	"github.com/jumpserver-dev/sdk-go/model"
)

var ErrNoAvailable = errors.New("no available domain")

const (
	miniTimeout = 15 * time.Second
)

type DomainGateway struct {
	DstAddr string // 10.0.0.1:3389

	sshClient       *gossh.Client
	jumpClient      *gossh.Client
	SelectedGateway *model.Gateway
	Destination     *model.Gateway

	ln net.Listener

	once sync.Once
}

func (d *DomainGateway) run() {
	defer d.closeOnce()
	for {
		con, err := d.ln.Accept()
		if err != nil {
			break
		}
		logger.Infof("Accept new conn by SSH forwarder %s ", d.Name())
		go d.handlerConn(con)
	}
	logger.Infof("Stop proxy by SSH forwarder %s", d.Name())
}

func (d *DomainGateway) handlerConn(srcCon net.Conn) {
	defer srcCon.Close()
	dstCon, err := d.sshClient.Dial("tcp", d.DstAddr)
	if err != nil {
		logger.Errorf("Failed gateway dial %s: %s ",
			d.DstAddr, err.Error())
		return
	}
	defer dstCon.Close()
	go func() {
		_, _ = io.Copy(dstCon, srcCon)
		_ = dstCon.Close()
	}()
	_, _ = io.Copy(srcCon, dstCon)
	logger.Infof("Gateway end proxy %s", d.DstAddr)
}

func (d *DomainGateway) Start() (err error) {
	if !d.getAvailableGateway() {
		return ErrNoAvailable
	}
	localIP := common.CurrentLocalIP()
	d.ln, err = net.Listen("tcp", net.JoinHostPort(localIP, "0"))
	if err != nil {
		_ = d.sshClient.Close()
		return err
	}
	go d.run()
	return nil
}

func (d *DomainGateway) GetListenAddr() *net.TCPAddr {
	return d.ln.Addr().(*net.TCPAddr)
}

func (d *DomainGateway) getAvailableGateway() bool {
	if d.Destination != nil {
		sshClient, jumpClient, err := d.createDestinationSSHClient()
		if err != nil {
			logger.Errorf("Dial SSH destination %s err: %s", d.Destination.Name, err)
			return false
		}
		d.sshClient = sshClient
		d.jumpClient = jumpClient
		return true
	}
	if d.SelectedGateway != nil {
		sshClient, err := d.createGatewaySSHClient(d.SelectedGateway)
		if err != nil {
			logger.Errorf("Dial select gateway %s err: %s ", d.SelectedGateway.Name, err)
			return false
		}
		logger.Debugf("Dial select gateway %s success", d.SelectedGateway.Name)
		d.sshClient = sshClient
		return true
	}
	return false
}

func (d *DomainGateway) createDestinationSSHClient() (*gossh.Client, *gossh.Client, error) {
	if d.SelectedGateway == nil {
		client, err := d.createGatewaySSHClient(d.Destination)
		return client, nil, err
	}
	jumpClient, err := d.createGatewaySSHClient(d.SelectedGateway)
	if err != nil {
		return nil, nil, err
	}
	addr := gatewaySSHAddress(d.Destination)
	conn, err := jumpClient.Dial("tcp", addr)
	if err != nil {
		_ = jumpClient.Close()
		return nil, nil, err
	}
	_ = conn.SetDeadline(time.Now().Add(miniTimeout))
	clientConn, chans, reqs, err := gossh.NewClientConn(
		conn, addr, gatewaySSHConfig(d.Destination),
	)
	if err != nil {
		_ = conn.Close()
		_ = jumpClient.Close()
		return nil, nil, err
	}
	_ = conn.SetDeadline(time.Time{})
	return gossh.NewClient(clientConn, chans, reqs), jumpClient, nil
}

func (d *DomainGateway) createGatewaySSHClient(gateway *model.Gateway) (*gossh.Client, error) {
	return gossh.Dial("tcp", gatewaySSHAddress(gateway), gatewaySSHConfig(gateway))
}

func gatewaySSHConfig(gateway *model.Gateway) *gossh.ClientConfig {
	auths := make([]gossh.AuthMethod, 0, 3)
	loginAccount := gateway.Account
	if loginAccount.IsSSHKey() {
		if signer, err1 := gossh.ParsePrivateKey([]byte(loginAccount.Secret)); err1 == nil {
			auths = append(auths, gossh.PublicKeys(signer))
		} else {
			logger.Errorf("Domain gateway Parse private key error: %s", err1)
		}
	} else {
		auths = append(auths, gossh.Password(loginAccount.Secret))
		auths = append(auths, gossh.KeyboardInteractive(func(user, instruction string,
			questions []string, echos []bool) (answers []string, err error) {
			return []string{loginAccount.Secret}, nil
		}))
	}
	return &gossh.ClientConfig{
		User:              loginAccount.Username,
		Auth:              auths,
		HostKeyCallback:   NewTrustHostKeyCallback(),
		Config:            createSSHConfig(),
		Timeout:           miniTimeout,
		HostKeyAlgorithms: allHostKeyAlgorithms(),
	}
}

func gatewaySSHAddress(gateway *model.Gateway) string {
	port := gateway.Protocols.GetProtocolPort("ssh")
	return net.JoinHostPort(gateway.Address, strconv.Itoa(port))
}

func (d *DomainGateway) Name() string {
	if d.Destination != nil {
		return d.Destination.Name
	}
	if d.SelectedGateway != nil {
		return d.SelectedGateway.Name
	}
	return "unknown"
}
func (d *DomainGateway) Stop() {
	d.closeOnce()
}

func (d *DomainGateway) closeOnce() {
	d.once.Do(func() {
		if d.ln != nil {
			_ = d.ln.Close()
		}
		if d.sshClient != nil {
			_ = d.sshClient.Close()
		}
		if d.jumpClient != nil {
			_ = d.jumpClient.Close()
		}
	})
}

func NewTrustHostKeyCallback() gossh.HostKeyCallback {
	return func(hostname string, remote net.Addr, key gossh.PublicKey) error {
		return nil
	}
}

func createSSHConfig() gossh.Config {
	var cfg gossh.Config
	cfg.SetDefaults()
	algos := gossh.SupportedAlgorithms()
	insecureAlgos := gossh.InsecureAlgorithms()
	ciphers := make([]string, 0, len(algos.Ciphers)+len(insecureAlgos.Ciphers))
	/*
		Change the ciphers order, placing aes128-ctr first.
		Compatible with old ssh servers.
	*/
	ciphers = append(ciphers, gossh.CipherAES128CTR)
	ciphers = append(ciphers, insecureAlgos.Ciphers...)
	ciphers = append(ciphers, algos.Ciphers...)
	keyExchanges := make([]string, 0, len(algos.KeyExchanges)+len(insecureAlgos.KeyExchanges))
	keyExchanges = append(keyExchanges, insecureAlgos.KeyExchanges...)
	keyExchanges = append(keyExchanges, algos.KeyExchanges...)
	cfg.Ciphers = ciphers
	cfg.KeyExchanges = keyExchanges
	return cfg
}

func allHostKeyAlgorithms() []string {
	supportedAlgos := gossh.SupportedAlgorithms()
	insecureAlgos := gossh.InsecureAlgorithms()
	hostKeyAlgos := make([]string, 0, len(supportedAlgos.HostKeys)+len(insecureAlgos.HostKeys)+1)
	/*
		Change the algorithm order, placing KeyAlgoED25519 first.
		Compatible with certain SSH servers.
	*/
	hostKeyAlgos = append(hostKeyAlgos, gossh.KeyAlgoED25519)
	hostKeyAlgos = append(hostKeyAlgos, supportedAlgos.HostKeys...)
	hostKeyAlgos = append(hostKeyAlgos, insecureAlgos.HostKeys...)
	return hostKeyAlgos
}
