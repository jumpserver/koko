package exchange

import (
	"context"
	"net"
	"strings"

	"github.com/jumpserver/koko/pkg/config"
	"github.com/jumpserver/koko/pkg/logger"
)

var ex Exchanger

func Initial(ctx context.Context) {
	conf := config.GetConf()
	var err error

	switch strings.ToLower(conf.ShareRoomType) {
	case "redis":
		ex, err = NewRedisExchange(Config{
			Addr:     net.JoinHostPort(conf.RedisHost, conf.RedisPort),
			Password: conf.RedisPassword,
			Clusters: conf.RedisClusters,
			DBIndex:  conf.RedisDBIndex,
		})

	default:
		ex, err = NewLocalExchange()
	}
	logger.Infof("Exchange share room type: %s", conf.ShareRoomType)
	if err != nil {
		logger.Fatal(err)
	}
}

func GetExchange() Exchanger {
	return ex
}

func StopExchange() {
	ex.Close()
}
