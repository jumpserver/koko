package dbconn

import (
	"crypto/tls"
	"errors"
	"net"
	"strings"

	db2 "github.com/go-db2/go-db2"
)

// DB2Config preserves the complete database name and authorized dialer before
// the driver constructs its DRDA session. Callers own timeout and session policy.
func DB2Config(c Config, secure *tls.Config) (*db2.Config, error) {
	// DRDA's RDBNAM is limited to 18 bytes; the driver otherwise truncates it.
	if c.Host == "" || strings.TrimSpace(c.Database) == "" || len(c.Database) > 18 || c.Port < 1 || c.Port > 65535 {
		return nil, errors.New("invalid Db2 connection configuration")
	}
	for _, value := range []string{c.Host, c.Database, c.User, c.Password} {
		if strings.ContainsRune(value, 0) {
			return nil, errors.New("invalid Db2 connection configuration")
		}
	}
	cfg := db2.NewConfig()
	cfg.Host, cfg.Port, cfg.Database = c.Host, c.Port, c.Database
	// The pinned driver's dialer formats host:port directly.
	if net.ParseIP(c.Host) != nil && strings.Contains(c.Host, ":") {
		cfg.Host = "[" + c.Host + "]"
	}
	cfg.User, cfg.Password, cfg.DialContext = c.User, c.Password, c.DialContext
	if secure != nil {
		cfg.UseSSL, cfg.TLSConfig = true, secure.Clone()
	} else {
		// The server-owned asset configuration explicitly selects plain TCP.
		cfg.AllowPlaintextAuth = true
	}
	return cfg, nil
}
