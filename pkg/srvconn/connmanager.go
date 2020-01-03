package srvconn

import (
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	gossh "golang.org/x/crypto/ssh"

	"github.com/jumpserver/koko/pkg/logger"
	"github.com/jumpserver/koko/pkg/model"
	"github.com/jumpserver/koko/pkg/service"
)

var (
	sshClients = make(map[string]*SSHClient)
	clientLock = new(sync.RWMutex)
)

var (
	supportedCiphers = []string{
		"aes128-ctr", "aes192-ctr", "aes256-ctr",
		"aes128-gcm@openssh.com",
		"chacha20-poly1305@openssh.com",
		"arcfour256", "arcfour128", "arcfour",
		"aes128-cbc",
		"3des-cbc"}

	supportedKexAlgos = []string{
		"diffie-hellman-group1-sha1",
		"diffie-hellman-group14-sha1", "ecdh-sha2-nistp256", "ecdh-sha2-nistp521",
		"ecdh-sha2-nistp384", "curve25519-sha256@libssh.org"}

	supportedHostKeyAlgos = []string{
		"ssh-rsa-cert-v01@openssh.com", "ssh-dss-cert-v01@openssh.com", "ecdsa-sha2-nistp256-cert-v01@openssh.com",
		"ecdsa-sha2-nistp384-cert-v01@openssh.com", "ecdsa-sha2-nistp521-cert-v01@openssh.com",
		"ssh-ed25519-cert-v01@openssh.com",
		"ecdsa-sha2-nistp256", "ecdsa-sha2-nistp384", "ecdsa-sha2-nistp521",
		"ssh-rsa", "ssh-dss",
		"ssh-ed25519",
	}
)

type SSHClient struct {
	client    *gossh.Client
	proxyConn gossh.Conn
	username  string

	ref int
	key string
	mu  *sync.RWMutex

	closed chan struct{}
}

func (s *SSHClient) RefCount() int {
	if s.isClosed() {
		return 0
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.ref
}

func (s *SSHClient) increaseRef() {
	if s.isClosed() {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ref++
}

func (s *SSHClient) decreaseRef() {
	if s.isClosed() {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ref == 0 {
		return
	}
	s.ref--
}

func (s *SSHClient) NewSession() (*gossh.Session, error) {
	return s.client.NewSession()
}

func (s *SSHClient) Close() error {
	select {
	case <-s.closed:
		return nil
	default:
		close(s.closed)
	}
	if s.proxyConn != nil {
		_ = s.proxyConn.Close()
	}
	s.mu.Lock()
	s.ref = 0
	s.mu.Unlock()
	return s.client.Close()
}

func (s *SSHClient) isClosed() bool {
	select {
	case <-s.closed:
		return true
	default:
		return false
	}
}

func KeepAlive(c *SSHClient, closed <-chan struct{}, keepInterval time.Duration) {
	t := time.NewTicker(keepInterval * time.Second)
	defer t.Stop()
	logger.Infof("SSH client %p keep alive start", c)
	defer logger.Infof("SSH client %p keep alive stop", c)
	for {
		select {
		case <-closed:
			return
		case <-t.C:
			_, _, err := c.client.SendRequest("keepalive@openssh.com", true, nil)
			if err != nil {
				logger.Errorf("SSH client %p keep alive err: %s", c, err.Error())
				_ = c.Close()
				RecycleClient(c)
				return
			}
		}

	}
}

type SSHClientConfig struct {
	Host           string        `json:"host"`
	Port           string        `json:"port"`
	User           string        `json:"user"`
	Password       string        `json:"password"`
	PrivateKey     string        `json:"private_key"`
	PrivateKeyPath string        `json:"private_key_path"`
	Timeout        time.Duration `json:"timeout"`
	Proxy          []*SSHClientConfig

	proxyConn gossh.Conn
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
		if signer, err := gossh.ParsePrivateKeyWithPassphrase([]byte(sc.PrivateKey), []byte(sc.Password)); err != nil {
			err = fmt.Errorf("parse private key error: %s", err)
			logger.Error(err.Error())
		} else {
			authMethods = append(authMethods, gossh.PublicKeys(signer))
		}
	}
	config = &gossh.ClientConfig{
		User:              sc.User,
		Auth:              authMethods,
		HostKeyCallback:   gossh.InsecureIgnoreHostKey(),
		Config:            gossh.Config{Ciphers: supportedCiphers, KeyExchanges: supportedKexAlgos},
		Timeout:           sc.Timeout,
		HostKeyAlgorithms: supportedHostKeyAlgos,
	}
	return config, nil
}

func (sc *SSHClientConfig) DialProxy() (client *gossh.Client, err error) {
	for _, p := range sc.Proxy {
		client, err = p.Dial()
		if err == nil {
			logger.Debugf("Connect proxy host %s:%s success", p.Host, p.Port)
			return
		} else {
			logger.Errorf("Connect proxy host %s:%s error: %s", p.Host, p.Port, err)
		}
	}
	return
}

func (sc *SSHClientConfig) Dial() (client *gossh.Client, err error) {
	cfg, err := sc.Config()
	if err != nil {
		return
	}
	if len(sc.Proxy) > 0 {
		logger.Debugf("Dial host proxy first")
		proxyClient, err := sc.DialProxy()
		if err != nil {
			err = errors.New("connect proxy host error 1: " + err.Error())
			logger.Error("Connect proxy host error 1: ", err.Error())
			return client, err
		}
		proxySock, err := proxyClient.Dial("tcp", net.JoinHostPort(sc.Host, sc.Port))
		if err != nil {
			err = errors.New(fmt.Sprintf("tcp connect host %s:%s error 2: %s", sc.Host, sc.Port, err.Error()))
			logger.Errorf("Tcp connect host %s:%s error 2: %s", sc.Host, sc.Port, err.Error())
			return client, err
		}
		proxyConn, chans, reqs, err := gossh.NewClientConn(proxySock, net.JoinHostPort(sc.Host, sc.Port), cfg)
		if err != nil {
			err = errors.New(fmt.Sprintf("ssh connect host %s:%s error 3: %s", sc.Host, sc.Port, err.Error()))
			logger.Errorf("SSH Connect host %s:%s error 3: %s", sc.Host, sc.Port, err.Error())
			return client, err
		}
		sc.proxyConn = proxyConn
		client = gossh.NewClient(proxyConn, chans, reqs)
	} else {
		logger.Debugf("Dial host %s:%s", sc.Host, sc.Port)
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

func MakeConfig(asset *model.Asset, systemUser *model.SystemUser, timeout time.Duration) (conf *SSHClientConfig) {
	proxyConfigs := make([]*SSHClientConfig, 0)
	// 如果有网关则从网关中连接
	if asset.Domain != "" {
		domain := service.GetDomainWithGateway(asset.Domain)
		if domain.ID != "" && len(domain.Gateways) > 0 {
			for _, gateway := range domain.Gateways {
				proxyConfigs = append(proxyConfigs, &SSHClientConfig{
					Host:       gateway.IP,
					Port:       strconv.Itoa(gateway.Port),
					User:       gateway.Username,
					Password:   gateway.Password,
					PrivateKey: gateway.PrivateKey,
					Timeout:    timeout,
				})
			}
		}
	}
	if systemUser.Password == "" && systemUser.PrivateKey == "" && systemUser.LoginMode != model.LoginModeManual {
		info := service.GetSystemUserAssetAuthInfo(systemUser.ID, asset.ID)
		systemUser.Password = info.Password
		systemUser.PrivateKey = info.PrivateKey
	}
	conf = &SSHClientConfig{
		Host:       asset.IP,
		Port:       strconv.Itoa(asset.ProtocolPort("ssh")),
		User:       systemUser.Username,
		Password:   systemUser.Password,
		PrivateKey: systemUser.PrivateKey,
		Timeout:    timeout,
		Proxy:      proxyConfigs,
	}
	return
}

func newClient(asset *model.Asset, systemUser *model.SystemUser, timeout time.Duration) (client *SSHClient, err error) {
	sshConfig := MakeConfig(asset, systemUser, timeout)
	conn, err := sshConfig.Dial()
	if err != nil {
		return nil, err
	}
	closed := make(chan struct{})
	client = &SSHClient{client: conn, proxyConn: sshConfig.proxyConn,
		username: systemUser.Username,
		mu:       new(sync.RWMutex),
		ref:      1,
		closed:   closed}
	go KeepAlive(client, closed, 60)
	return client, nil
}

func NewClient(user *model.User, asset *model.Asset, systemUser *model.SystemUser, timeout time.Duration,
	useCache bool) (client *SSHClient, err error) {

	client, err = newClient(asset, systemUser, timeout)
	if err == nil && useCache {
		key := MakeReuseSSHClientKey(user, asset, systemUser)
		setClientCache(key, client)
	}
	return
}

func searchSSHClientFromCache(prefixKey string) (client *SSHClient, ok bool) {
	clientLock.Lock()
	defer clientLock.Unlock()
	for key, cacheClient := range sshClients {
		if strings.HasPrefix(key, prefixKey) {
			cacheClient.increaseRef()
			return cacheClient, true
		}
	}
	return
}

func GetClientFromCache(key string) (client *SSHClient, ok bool) {
	clientLock.Lock()
	defer clientLock.Unlock()
	client, ok = sshClients[key]
	if ok {
		client.increaseRef()
	}
	return
}

func setClientCache(key string, client *SSHClient) {
	clientLock.Lock()
	sshClients[key] = client
	client.key = key
	clientLock.Unlock()
}

func RecycleClient(client *SSHClient) {
	// decrease client ref; if ref==0, delete Cache, close client.
	if client == nil {
		return
	}
	client.decreaseRef()
	if client.RefCount() == 0 {
		clientLock.Lock()
		delete(sshClients, client.key)
		clientLock.Unlock()
		err := client.Close()
		if err != nil {
			logger.Errorf("Close SSH client %p err: %s ", client, err.Error())
		} else {
			logger.Infof("Success to close SSH client %p", client)
		}
	} else {
		logger.Debugf("SSH client %p ref -1. current ref: %d", client, client.RefCount())
	}
}

func MakeReuseSSHClientKey(user *model.User, asset *model.Asset, systemUser *model.SystemUser) string {
	return fmt.Sprintf("%s_%s_%s_%s", user.ID, asset.ID, systemUser.ID, systemUser.Username)
}
