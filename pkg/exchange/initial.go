package exchange

import (
	"net"
	"os"
	"path/filepath"
	"strings"

	"github.com/jumpserver/koko/pkg/config"
	"github.com/jumpserver/koko/pkg/logger"
)

var manager RoomManager

func Initial() {
	conf := config.GetConf()
	var (
		err error
	)

	switch strings.ToLower(conf.ShareRoomType) {
	case "redis":
		existFile := func(path string) string {
			if info, err2 := os.Stat(path); err2 == nil && !info.IsDir() {
				return path
			}
			return ""
		}
		sslCaPath := filepath.Join(conf.CertsFolderPath, "redis_ca.crt")
		sslCertPath := filepath.Join(conf.CertsFolderPath, "redis_client.crt")
		sslKeyPath := filepath.Join(conf.CertsFolderPath, "redis_client.key")
		manager, err = newRedisManager(Config{
			Addr:     net.JoinHostPort(conf.RedisHost, conf.RedisPort),
			Password: conf.RedisPassword,
			Clusters: conf.RedisClusters,
			DBIndex:  conf.RedisDBIndex,

			SentinelPassword: conf.RedisSentinelPassword,
			SentinelsHost:    conf.RedisSentinelHosts,
			UseSSL:           conf.RedisUseSSL,
			SSLCa:            existFile(sslCaPath),
			SSLCert:          existFile(sslCertPath),
			SSLKey:           existFile(sslKeyPath),
		})

	default:
		manager = newLocalManager()
	}
	logger.Infof("Exchange share room type: %s", conf.ShareRoomType)
	if err != nil {
		logger.Fatal(err)
	}
}

func Register(r *Room) {
	manager.Add(r)
}

func UnRegister(r *Room) {
	manager.Delete(r)
}

func GetRoom(roomId string) *Room {
	return manager.Get(roomId)
}
