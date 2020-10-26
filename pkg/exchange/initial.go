package exchange

import (
	"context"
	"net"
	"strings"

	"github.com/jumpserver/koko/pkg/config"
	"github.com/jumpserver/koko/pkg/logger"
)

var exchangeCache roomCache

func Initial(ctx context.Context) {
	conf := config.GetConf()
	var (
		err error
	)

	switch strings.ToLower(conf.ShareRoomType) {
	case "redis":
		exchangeCache, err = newRedisCache(Config{
			Addr:     net.JoinHostPort(conf.RedisHost, conf.RedisPort),
			Password: conf.RedisPassword,
			Clusters: conf.RedisClusters,
			DBIndex:  conf.RedisDBIndex,
		})

	default:
		exchangeCache = newLocalCache()
	}
	logger.Infof("Exchange share room type: %s", conf.ShareRoomType)
	if err != nil {
		logger.Fatal(err)
	}
}

func Register(r *Room) {
	exchangeCache.Add(r)
}

func UnRegister(r *Room) {
	exchangeCache.Delete(r)
}

func GetRoom(roomId string) *Room {
	return exchangeCache.Get(roomId)
}
