package srvconn

import (
	"cocogo/pkg/service"
	"errors"
	"fmt"
	"net"
	"strconv"
	"sync"
	"time"

	gossh "golang.org/x/crypto/ssh"

	"cocogo/pkg/logger"
	"cocogo/pkg/model"
)

var (
	sshClients        = make(map[string]*gossh.Client)
	clientsRefCounter = make(map[*gossh.Client]int)
	clientLock        = new(sync.RWMutex)
)

type SSHClientConfig struct {
	Host           string
	Port           string
	User           string
	Password       string
	PrivateKey     string
	PrivateKeyPath string
	Timeout        time.Duration
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
		if signer, err := gossh.ParsePrivateKey([]byte(sc.PrivateKey)); err != nil {
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
		Timeout:         sc.Timeout,
	}
	return config, nil
}

func (sc *SSHClientConfig) DialProxy() (client *gossh.Client, err error) {
	for _, p := range sc.Proxy {
		client, err = p.Dial()
		if err == nil {
			return
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

func newClient(asset *model.Asset, systemUser *model.SystemUser, timeout time.Duration) (client *gossh.Client, err error) {
	proxyConfigs := make([]*SSHClientConfig, 0)
	// 如果有网关则从网关中连接
	if asset.Domain != "" {
		domain := service.GetDomainWithGateway(asset.Domain)
		if domain.ID != "" && len(domain.Gateways) > 1 {
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
	sshConfig := SSHClientConfig{
		Host:       asset.IP,
		Port:       strconv.Itoa(asset.Port),
		User:       systemUser.Username,
		Password:   systemUser.Password,
		PrivateKey: systemUser.PrivateKey,
		Timeout:    timeout,
		Proxy:      proxyConfigs,
	}
	client, err = sshConfig.Dial()
	return
}

func NewClient(user *model.User, asset *model.Asset, systemUser *model.SystemUser, timeout time.Duration) (client *gossh.Client, err error) {
	key := fmt.Sprintf("%s_%s_%s", user.ID, asset.ID, systemUser.ID)
	clientLock.RLock()
	client, ok := sshClients[key]
	clientLock.RUnlock()

	var u = user.Username
	var ip = asset.IP
	var sysName = systemUser.Username

	if ok {
		clientLock.Lock()
		clientsRefCounter[client]++

		var counter = clientsRefCounter[client]
		logger.Infof("Reuse connection: %s->%s@%s\n ref: %d", u, sysName, ip, counter)
		clientLock.Unlock()
		return client, nil
	}

	client, err = newClient(asset, systemUser, timeout)
	if err == nil {
		clientLock.Lock()
		sshClients[key] = client
		clientsRefCounter[client] = 1
		clientLock.Unlock()
	}
	return
}

func GetClientFromCache(user *model.User, asset *model.Asset, systemUser *model.SystemUser) (client *gossh.Client) {
	key := fmt.Sprintf("%s_%s_%s", user.ID, asset.ID, systemUser.ID)
	clientLock.Lock()
	defer clientLock.Unlock()
	client, ok := sshClients[key]
	if !ok {
		return
	}
	clientsRefCounter[client]++
	return
}

func RecycleClient(client *gossh.Client) {
	clientLock.Lock()
	defer clientLock.Unlock()

	if counter, ok := clientsRefCounter[client]; ok {
		if counter == 1 {
			logger.Debug("Recycle client: close it")
			_ = client.Close()
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
		} else {
			logger.Debug("Recycle client: ref -1")
			clientsRefCounter[client]--
		}
	}
}
