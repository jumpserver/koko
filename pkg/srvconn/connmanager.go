package srvconn

import (
	"fmt"
)

var sshManager = newSSHManager()

func searchSSHClientFromCache(prefixKey string) (client *SSHClient, ok bool) {
	return sshManager.searchSSHClientFromCache(prefixKey)
}

func GetClientFromCache(key string) (client *SSHClient, ok bool) {
	return sshManager.getClientFromCache(key)
}

func AddClientCache(key string, client *SSHClient) {
	sshManager.AddClientCache(key, client)
}

func MakeReuseSSHClientKey(userId, assetId, account,
	ip, username string) string {
	return fmt.Sprintf("%s_%s_%s_%s_%s", userId, assetId,
		account, ip, username)
}
