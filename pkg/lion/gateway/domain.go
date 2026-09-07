package gateway

import (
	"context"
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

	mu     sync.Mutex
	once   sync.Once
	ctx    context.Context
	cancel context.CancelFunc
	closed bool
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
	ctx, cancel := context.WithTimeout(d.ctx, miniTimeout)
	defer cancel()
	// Closing the SSH transport also interrupts a channel-open request whose
	// server never replies; DialContext alone leaves that request running.
	stop := context.AfterFunc(ctx, d.closeOnce)
	dstCon, err := d.sshClient.Dial("tcp", d.DstAddr)
	if !stop() || ctx.Err() != nil {
		d.closeOnce()
		if dstCon != nil {
			_ = dstCon.Close()
		}
		return
	}
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

func (d *DomainGateway) Start() error {
	return d.StartContext(context.Background())
}

// StartContext starts forwarding until Stop is called or ctx is canceled.
func (d *DomainGateway) StartContext(ctx context.Context) (err error) {
	d.mu.Lock()
	if d.closed || d.cancel != nil {
		d.mu.Unlock()
		return errors.New("domain gateway already started or stopped")
	}
	d.ctx, d.cancel = context.WithCancel(ctx)
	ctx = d.ctx
	d.mu.Unlock()

	sshClient, jumpClient, err := d.getAvailableGateway(ctx)
	if err != nil {
		d.closeOnce()
		return err
	}
	var ln net.Listener
	defer func() {
		if err != nil {
			if ln != nil {
				_ = ln.Close()
			}
			if jumpClient != nil {
				_ = jumpClient.Close()
			}
			_ = sshClient.Close()
			d.closeOnce()
		}
	}()
	localIP := common.CurrentLocalIP()
	ln, err = net.Listen("tcp", net.JoinHostPort(localIP, "0"))
	if err != nil {
		return err
	}
	d.mu.Lock()
	if err = ctx.Err(); err == nil {
		d.sshClient, d.jumpClient, d.ln = sshClient, jumpClient, ln
	}
	d.mu.Unlock()
	if err != nil {
		return err
	}
	go func() {
		<-ctx.Done()
		d.closeOnce()
	}()
	go d.run()
	return nil
}

func (d *DomainGateway) GetListenAddr() *net.TCPAddr {
	return d.ln.Addr().(*net.TCPAddr)
}

func (d *DomainGateway) getAvailableGateway(ctx context.Context) (*gossh.Client, *gossh.Client, error) {
	if d.Destination != nil {
		return d.createDestinationSSHClient(ctx)
	}
	if d.SelectedGateway != nil {
		client, err := d.createGatewaySSHClient(ctx, d.SelectedGateway)
		return client, nil, err
	}
	return nil, nil, ErrNoAvailable
}

func (d *DomainGateway) createDestinationSSHClient(ctx context.Context) (_ *gossh.Client, _ *gossh.Client, err error) {
	if d.SelectedGateway == nil {
		client, err := d.createGatewaySSHClient(ctx, d.Destination)
		return client, nil, err
	}
	jumpClient, err := d.createGatewaySSHClient(ctx, d.SelectedGateway)
	if err != nil {
		return nil, nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, miniTimeout)
	defer cancel()
	// An SSH channel does not implement deadlines. Close the underlying jump
	// transport to interrupt both channel opening and the destination handshake.
	stop := context.AfterFunc(ctx, func() { _ = jumpClient.Close() })
	defer func() {
		stop()
		if err != nil {
			_ = jumpClient.Close()
		}
	}()
	addr := gatewaySSHAddress(d.Destination)
	conn, err := jumpClient.Dial("tcp", addr)
	if err != nil {
		if ctx.Err() != nil {
			err = ctx.Err()
		}
		return nil, nil, err
	}
	client, err := newSSHClient(ctx, conn, addr, gatewaySSHConfig(d.Destination))
	if err != nil {
		return nil, nil, err
	}
	if !stop() || ctx.Err() != nil {
		_ = jumpClient.Close()
		_ = client.Close()
		return nil, nil, ctx.Err()
	}
	return client, jumpClient, nil
}

func (d *DomainGateway) createGatewaySSHClient(ctx context.Context, gateway *model.Gateway) (*gossh.Client, error) {
	ctx, cancel := context.WithTimeout(ctx, miniTimeout)
	defer cancel()
	addr := gatewaySSHAddress(gateway)
	conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, err
	}
	return newSSHClient(ctx, conn, addr, gatewaySSHConfig(gateway))
}

func newSSHClient(ctx context.Context, conn net.Conn, addr string, config *gossh.ClientConfig) (*gossh.Client, error) {
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	clientConn, chans, reqs, err := gossh.NewClientConn(conn, addr, config)
	if !stop() || ctx.Err() != nil {
		_ = conn.Close()
		return nil, ctx.Err()
	}
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	return gossh.NewClient(clientConn, chans, reqs), nil
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
		d.mu.Lock()
		defer d.mu.Unlock()
		d.closed = true
		if d.cancel != nil {
			d.cancel()
		}
		if d.ln != nil {
			_ = d.ln.Close()
		}
		if d.jumpClient != nil {
			_ = d.jumpClient.Close()
		}
		if d.sshClient != nil {
			_ = d.sshClient.Close()
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
