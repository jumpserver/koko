package srvconn

import (
	"fmt"
	"strconv"
	"sync"

	gossh "golang.org/x/crypto/ssh"

	"cocogo/pkg/config"
	"cocogo/pkg/logger"
	"cocogo/pkg/model"
)

var (
	sshClients        = make(map[string]*gossh.Client)
	clientsRefCounter = make(map[*gossh.Client]int)
	clientLock        = new(sync.RWMutex)
)

func newClient(user *model.User, asset *model.Asset,
	systemUser *model.SystemUser) (client *gossh.Client, err error) {
	cfg := SSHClientConfig{
		Host:       asset.Ip,
		Port:       strconv.Itoa(asset.Port),
		User:       systemUser.Username,
		Password:   systemUser.Password,
		PrivateKey: systemUser.PrivateKey,
		Overtime:   config.GetConf().SSHTimeout,
	}
	client, err = cfg.Dial()
	return
}

func NewClient(user *model.User, asset *model.Asset, systemUser *model.SystemUser) (client *gossh.Client, err error) {
	key := fmt.Sprintf("%s_%s_%s", user.ID, asset.Id, systemUser.Id)
	clientLock.RLock()
	client, ok := sshClients[key]
	clientLock.RUnlock()

	var u = user.Username
	var ip = asset.Ip
	var sysName = systemUser.Username

	if ok {
		clientLock.Lock()
		clientsRefCounter[client]++

		var counter = clientsRefCounter[client]
		logger.Infof("Reuse connection: %s->%s@%s\n ref: %d", u, sysName, ip, counter)
		clientLock.Unlock()
		return client, nil
	}

	client, err = newClient(user, asset, systemUser)
	if err == nil {
		clientLock.Lock()
		sshClients[key] = client
		clientsRefCounter[client] = 1
		clientLock.Unlock()
	}
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
