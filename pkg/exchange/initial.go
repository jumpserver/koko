package exchange

import (
	"context"
	"net"
	"strings"

	"github.com/jumpserver/koko/pkg/config"
	"github.com/jumpserver/koko/pkg/logger"
)

var manager roomManager

func Initial(ctx context.Context) {
	conf := config.GetConf()
	var (
		err error
	)

	switch strings.ToLower(conf.ShareRoomType) {
	case "redis":
		manager, err = newRedisManager(Config{
			Addr:     net.JoinHostPort(conf.RedisHost, conf.RedisPort),
			Password: conf.RedisPassword,
			Clusters: conf.RedisClusters,
			DBIndex:  conf.RedisDBIndex,
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
