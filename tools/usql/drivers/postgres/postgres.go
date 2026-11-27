// Package postgres retains the upstream PostgreSQL metadata/COPY support and
// adds the managed gateway and explicit prefer-mode connection options.
package postgres

import (
	"context"
	"crypto/tls"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"net"
	"time"

	"github.com/lib/pq"
	"github.com/xo/dburl"
	"github.com/xo/usql/drivers"
	_ "github.com/xo/usql/drivers/postgres"
	"github.com/xo/usql/text"
)

func init() {
	registry := drivers.Available()
	for _, name := range []string{"postgres", "cockroachdb", "redshift"} {
		d := registry[name]
		upstream := d.Open
		d.Open = func(ctx context.Context, source *dburl.URL, stdout, stderr func() io.Writer) (func(string, string) (*sql.DB, error), error) {
			q := source.Query()
			if !q.Has("hostaddr") && q.Get("sslmode") != "prefer" && !caOnlyMode(q) {
				return upstream(ctx, source, stdout, stderr)
			}
			return func(_, _ string) (*sql.DB, error) {
				connector, err := managedConnector(source)
				if err != nil {
					return nil, err
				}
				notices := pq.ConnectorWithNoticeHandler(connector, func(n *pq.Error) {
					fmt.Fprintln(stderr(), n.Severity+": ", n.Message)
					if n.Hint != "" {
						fmt.Fprintln(stderr(), "HINT: ", n.Hint)
					}
				})
				notifications := pq.ConnectorWithNotificationHandler(notices, func(n *pq.Notification) {
					payload := ""
					if n.Extra != "" {
						payload = fmt.Sprintf(text.NotificationPayload, n.Extra)
					}
					fmt.Fprintln(stdout(), fmt.Sprintf(text.NotificationReceived, n.Channel, payload, n.BePid))
				})
				return sql.OpenDB(notifications), nil
			}, nil
		}
		registry[name] = d
	}
}

func managedConnector(source *dburl.URL) (driver.Connector, error) {
	u := *source
	q := u.Query()
	hostaddr := q.Get("hostaddr")
	q.Del("hostaddr") // lib/pq otherwise sends this as a server startup variable.
	if hostaddr != "" && net.ParseIP(hostaddr) == nil {
		return nil, errors.New("PostgreSQL hostaddr must be an IP address")
	}
	prefer := q.Get("sslmode") == "prefer"
	if prefer {
		q.Set("sslmode", "require")
	}
	dialer := addressDialer{host: hostaddr}
	if caOnlyMode(q) {
		// lib/pq 1.10.9 incorrectly applies hostname verification to verify-ca
		// when SNI is enabled. Negotiate here to retain SNI, chain-only policy
		// and encrypted cancellation, without patching the upstream driver.
		var err error
		dialer.tls, err = certificateAuthorityTLS(q, u.Hostname())
		if err != nil {
			return nil, err
		}
		q.Set("sslmode", "disable") // The custom dialer owns SSLRequest/TLS.
		for _, key := range []string{"sslrootcert", "sslcert", "sslkey", "sslinline", "sslsni"} {
			q.Del(key)
		}
	}
	u.RawQuery = q.Encode()
	parsed, err := dburl.Parse(u.String())
	if err != nil {
		return nil, errors.New("invalid PostgreSQL connection options")
	}
	primary, err := pq.NewConnector(parsed.DSN)
	if err != nil {
		return nil, err
	}
	if hostaddr != "" || dialer.tls != nil {
		// Keep the logical URL host for SNI/certificate verification while all
		// connections (including cancellation) dial the selected SSH forward.
		primary.Dialer(dialer)
	}
	if !prefer {
		return primary, nil
	}
	q.Set("sslmode", "disable")
	u.RawQuery = q.Encode()
	parsed, err = dburl.Parse(u.String())
	if err != nil {
		return nil, err
	}
	fallback, err := pq.NewConnector(parsed.DSN)
	if err != nil {
		return nil, err
	}
	if hostaddr != "" {
		fallback.Dialer(addressDialer{host: hostaddr})
	}
	return &preferConnector{primary, fallback}, nil
}

type preferConnector struct {
	primary, fallback driver.Connector
}

func (c *preferConnector) Connect(ctx context.Context) (driver.Conn, error) {
	conn, err := c.primary.Connect(ctx)
	// Only an explicit prefer plus an explicit SSLRequest refusal permits
	// plaintext. Certificate errors/authentication errors never trigger retry.
	if errors.Is(err, pq.ErrSSLNotSupported) {
		return c.fallback.Connect(ctx)
	}
	return conn, err
}

func (c *preferConnector) Driver() driver.Driver { return c.primary.Driver() }

type addressDialer struct {
	host string
	tls  *tls.Config
}

func (d addressDialer) Dial(network, address string) (net.Conn, error) {
	return d.DialContext(context.Background(), network, address)
}

func (d addressDialer) DialTimeout(network, address string, timeout time.Duration) (net.Conn, error) {
	if timeout == 0 {
		return d.Dial(network, address)
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return d.DialContext(ctx, network, address)
}

func (d addressDialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	if d.host != "" {
		_, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, fmt.Errorf("invalid PostgreSQL gateway address")
		}
		address = net.JoinHostPort(d.host, port)
	}
	conn, err := (&net.Dialer{}).DialContext(ctx, network, address)
	if err != nil || d.tls == nil {
		return conn, err
	}
	secure, err := negotiateTLS(ctx, conn, d.tls)
	if err != nil {
		conn.Close()
		return nil, err
	}
	return secure, nil
}
