package sessiontools

import (
	"context"
	"database/sql/driver"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"strconv"
	"strings"

	dm "gitee.com/chunanyong/dm"
	db2 "github.com/go-db2/go-db2"
	"github.com/jumpserver/koko/pkg/srvconn"
)

func newAgentDB2DamengConnector(config DatabaseConfig) (*agentSQLRelayConnector, error) {
	c := &agentSQLRelayConnector{
		address: net.JoinHostPort(config.Host, strconv.Itoa(config.Port)), dial: config.DialContext,
	}
	switch config.Protocol {
	case srvconn.ProtocolDB2:
		if strings.TrimSpace(config.Database) == "" || len(config.Database) > 18 {
			return nil, errors.New("Db2 requires a database name of at most 18 bytes")
		}
		cfg := db2.NewConfig()
		cfg.User, cfg.Password, cfg.Database = config.Username, config.Password, config.Database
		cfg.UseSSL = config.UseSSL
		if config.UseSSL {
			if config.AllowInvalidCert {
				return nil, errors.New("Db2 requires server certificate verification")
			}
			if config.ServerName == "" {
				config.ServerName = config.Host
			}
			tlsConfig, err := databaseTLSConfig(config)
			if err != nil {
				return nil, err
			}
			cfg.TLSConfig = tlsConfig
		}
		c.driver = &db2.Driver{}
		c.connector = func(host string, port int) (driver.Connector, error) {
			local := *cfg
			local.Host, local.Port = host, port
			return db2.NewConnector(&local), nil
		}
	case srvconn.ProtocolDameng:
		// The pinned DM driver hardcodes InsecureSkipVerify and cannot consume
		// our CA policy. Reject required TLS instead of silently dropping it.
		if config.UseSSL || config.CACert != "" || config.ClientCert != "" || config.ClientKey != "" {
			return nil, errors.New("Dameng MCP TLS certificate verification is unsupported by the driver")
		}
		if strings.ContainsAny(config.Username, ":\x00\r\n") || strings.ContainsAny(config.Database, "/@?&\x00\r\n") {
			return nil, errors.New("unsupported Dameng username or schema characters")
		}
		dmDriver := &dm.DmDriver{}
		c.driver = dmDriver
		c.connector = func(host string, port int) (driver.Connector, error) {
			// DM parses raw userinfo without URL-decoding it. The final query
			// delimiter keeps '?' and '@' in a password from changing options.
			dsn := "dm://" + config.Username + ":" + config.Password + "@" + net.JoinHostPort(host, strconv.Itoa(port))
			if config.Database != "" {
				dsn += "/" + config.Database
			}
			dsn += "?host=" + host + "&port=" + strconv.Itoa(port) +
				"&driverReconnect=false&doSwitch=0&switchTimes=1&switchInterval=0" +
				"&rwSeparate=0&rwHA=false&rwStandby=false&cluster=0&compress=0&logLevel=0&statEnable=false"
			return dmDriver.OpenConnector(dsn)
		}
		dial := c.dial
		c.dial = func(ctx context.Context, network, address string) (net.Conn, error) {
			conn, err := dial(ctx, network, address)
			if err != nil {
				return nil, err
			}
			return &agentDamengWireConn{Conn: conn}, nil
		}
	default:
		return nil, errors.New("unsupported agent database protocol")
	}
	return c, nil
}

// Both pinned drivers need a loopback relay: Db2 has no injectable dialer,
// and DM drops the caller's context while connecting. Only one local socket
// is accepted, forwarding solely to the authorized, budgeted asset transport.
type agentSQLRelayConnector struct {
	driver    driver.Driver
	address   string
	dial      databaseDialer
	connector func(host string, port int) (driver.Connector, error)
}

func (c *agentSQLRelayConnector) Driver() driver.Driver { return c.driver }

func (c *agentSQLRelayConnector) Connect(ctx context.Context) (driver.Conn, error) {
	remote, err := c.dial(ctx, "tcp", c.address)
	if err != nil {
		return nil, err
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		_ = remote.Close()
		return nil, err
	}
	defer listener.Close()
	stop := context.AfterFunc(ctx, func() { _ = listener.Close(); _ = remote.Close() })
	defer stop()
	connector, err := c.connector("127.0.0.1", listener.Addr().(*net.TCPAddr).Port)
	if err != nil {
		_ = remote.Close()
		return nil, err
	}
	relayError := make(chan error, 1)
	go func() {
		local, err := listener.Accept()
		_ = listener.Close()
		if err != nil {
			_ = remote.Close()
			return
		}
		defer local.Close()
		defer remote.Close()
		go func() { _, _ = io.CopyBuffer(remote, local, make([]byte, 32<<10)); _ = remote.Close() }()
		_, err = io.CopyBuffer(local, remote, make([]byte, 32<<10))
		relayError <- err
	}()
	conn, err := connector.Connect(ctx)
	if err != nil {
		_ = remote.Close()
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		select {
		case relayErr := <-relayError:
			err = errors.Join(err, relayErr)
		default:
		}
	}
	return conn, err
}

// DM allocates its response body after reading a 64-byte header. Validate that
// length first and reject its unverified TLS negotiation before the driver
// upgrades the connection with certificate verification disabled.
type agentDamengWireConn struct {
	net.Conn
	header  [64]byte
	pending []byte
	body    uint32
	started bool
}

func (c *agentDamengWireConn) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if len(c.pending) != 0 {
		n := copy(p, c.pending)
		c.pending = c.pending[n:]
		return n, nil
	}
	if c.body != 0 {
		if uint64(len(p)) > uint64(c.body) {
			p = p[:c.body]
		}
		n, err := c.Conn.Read(p)
		c.body -= uint32(n)
		return n, err
	}
	if _, err := io.ReadFull(c.Conn, c.header[:]); err != nil {
		return 0, err
	}
	c.body = binary.LittleEndian.Uint32(c.header[6:10])
	if c.body > 16<<20 || (!c.started && binary.LittleEndian.Uint32(c.header[20:24]) != 0) {
		_ = c.Conn.Close()
		return 0, errors.New("Dameng response exceeds its limit or requires unsupported TLS verification")
	}
	c.started, c.pending = true, c.header[:]
	return c.Read(p)
}
