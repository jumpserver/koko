package srvconn

import (
	"errors"
	"fmt"
	"net"
	"strconv"
	"sync"
	"time"

	gossh "golang.org/x/crypto/ssh"

	"github.com/jumpserver/koko/pkg/logger"
	"github.com/jumpserver/koko/pkg/model"
	"github.com/jumpserver/koko/pkg/service"
)

var (
	sshClients        = make(map[string]*SSHClient)
	clientsRefCounter = make(map[*SSHClient]int)
	clientLock        = new(sync.RWMutex)
)

var (
	supportedCiphers = []string{
		"aes128-ctr", "aes192-ctr", "aes256-ctr",
		"aes128-gcm@openssh.com",
		"chacha20-poly1305@openssh.com",
		"arcfour256", "arcfour128", "arcfour",
		"aes128-cbc",
		"3des-cbc"}
)

type SSHClient struct {
	Client   *gossh.Client
	Username string
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
			return config, err
		} else {
			authMethods = append(authMethods, gossh.PublicKeys(signer))
		}
	}
	config = &gossh.ClientConfig{
		User:            sc.User,
		Auth:            authMethods,
		HostKeyCallback: gossh.InsecureIgnoreHostKey(),
		Config:          gossh.Config{Ciphers: supportedCiphers},
		Timeout:         sc.Timeout,
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
			err = errors.New("connect proxy host error 2: " + err.Error())
			logger.Error("Connect proxy host error 2: ", err.Error())
			return client, err
		}
		proxyConn, chans, reqs, err := gossh.NewClientConn(proxySock, net.JoinHostPort(sc.Host, sc.Port), cfg)
		if err != nil {
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
	if systemUser.Password == "" && systemUser.PrivateKey == "" {
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
	return &SSHClient{Client: conn, Username: systemUser.Username}, err
}

func NewClient(user *model.User, asset *model.Asset, systemUser *model.SystemUser, timeout time.Duration) (client *SSHClient, err error) {
	client = GetClientFromCache(user, asset, systemUser)
	if client != nil {
		return client, nil
	}

	key := fmt.Sprintf("%s_%s_%s", user.ID, asset.ID, systemUser.ID)
	client, err = newClient(asset, systemUser, timeout)
	if err == nil {
		clientLock.Lock()
		sshClients[key] = client
		clientsRefCounter[client] = 1
		clientLock.Unlock()
	}
	return
}

func GetClientFromCache(user *model.User, asset *model.Asset, systemUser *model.SystemUser) (client *SSHClient) {
	key := fmt.Sprintf("%s_%s_%s", user.ID, asset.ID, systemUser.ID)
	clientLock.Lock()
	defer clientLock.Unlock()
	client, ok := sshClients[key]
	if !ok {
		return
	}

	var u = user.Username
	var ip = asset.IP
	clientsRefCounter[client]++
	var counter = clientsRefCounter[client]

	logger.Infof("Reuse connection: %s->%s@%s ref: %d", u, client.Username, ip, counter)
	return
}

func RecycleClient(client *SSHClient) {
	clientLock.RLock()
	counter, ok := clientsRefCounter[client]
	clientLock.RUnlock()

	if ok {
		if counter == 1 {
			logger.Debug("Recycle client: close it")
			CloseClient(client)
		} else {
			clientLock.Lock()
			clientsRefCounter[client]--
			clientLock.Unlock()
			logger.Debugf("Recycle client: ref -1: %d", clientsRefCounter[client])
		}
	}
}

func CloseClient(client *SSHClient) {
	clientLock.Lock()
	defer clientLock.Unlock()

	delete(clientsRefCounter, client)
	var key string
	for k, v := range sshClients {
		if v == client {
			key = k
			break
		}
	}
	if key != "" {
		delete(sshClients, key)
	}
	_ = client.Client.Close()
}
