package dbconn

import (
	"context"
	"crypto/tls"
	"net"
	"strconv"

	goora "github.com/sijms/go-ora/v2"
)

// OracleConnector keeps the certificate identity separate from the dial target,
// including when an authorized gateway owns DNS and routing.
func OracleConnector(c Config, secure *tls.Config, options map[string]string) *goora.OracleConnector {
	host := c.Host
	if c.TLS.ServerName != "" {
		host = c.TLS.ServerName
	}
	connector := goora.NewConnector(goora.BuildUrl(host, c.Port, c.Database, c.User, c.Password, options)).(*goora.OracleConnector)
	if c.DialContext != nil || host != c.Host {
		connector.Dialer(fixedAddressDialer{address: net.JoinHostPort(c.Host, strconv.Itoa(c.Port)), dial: c.DialContext})
	}
	if secure != nil {
		connector.WithTLSConfig(secure)
	}
	return connector
}

type fixedAddressDialer struct {
	address string
	dial    func(context.Context, string, string) (net.Conn, error)
}

func (d fixedAddressDialer) DialContext(ctx context.Context, network, _ string) (net.Conn, error) {
	if d.dial != nil {
		return d.dial(ctx, network, d.address)
	}
	return (&net.Dialer{}).DialContext(ctx, network, d.address)
}
