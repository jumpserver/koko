package config

import (
	"crypto/rand"
	"math/big"
	"net"
	"strings"
)

const (
	GinCtxUserKey  = "JMS-CtxUserKey"
	GinSessionName = "session-Lion"
	GinSessionKey  = "SESSION"
)

const (
	ShareTypeRedis = "redis"
	ShareTypeLocal = "local"
)

const (
	LionDriveScopeUser    = "user"
	LionDriveScopeSession = "session"

	// Keep the original names available to the migrated Lion packages.
	DriverScopeUser    = LionDriveScopeUser
	DriverScopeSession = LionDriveScopeSession
)

const defaultLionReplayMaxSize = 1024 * 1024 * 300

func (c Config) SelectGuacdAddr() string {
	if c.GuacdAddrs == "" {
		return net.JoinHostPort(c.GuaHost, c.GuaPort)
	}

	addresses := make([]string, 0)
	for _, address := range strings.Split(c.GuacdAddrs, ",") {
		if address = strings.TrimSpace(address); address != "" {
			addresses = append(addresses, address)
		}
	}
	if len(addresses) == 0 {
		return net.JoinHostPort(c.GuaHost, c.GuaPort)
	}

	index, err := rand.Int(rand.Reader, big.NewInt(int64(len(addresses))))
	if err != nil {
		return addresses[0]
	}
	return addresses[index.Int64()]
}
