package srvconn

import (
	"fmt"
)

var sshManager = newSSHManager()

func GetClientFromCache(key string) (client *SSHClient, ok bool) {
	return sshManager.getClientFromCache(key)
}

func AddClientCache(key string, client *SSHClient) {
	sshManager.AddClientCache(key, client)
}

func ReleaseClientCacheKey(key string, client *SSHClient) {
	sshManager.ReleaseClientCacheKey(key, client)
}

func MakeReuseSSHClientKey(userId, assetId, account,
	ip, username string) string {
	return fmt.Sprintf("%s_%s_%s_%s_%s", userId, assetId,
		account, ip, username)
}
