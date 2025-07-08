package srvconn

import (
	"net"
	"net/url"
	"strconv"
	"time"

	"github.com/jumpserver/koko/pkg/localcommand"
)

var (
	_ ServerConnection = (*USQLConn)(nil)
)

func NewUSQLConnection(opts ...SqlOption) (*USQLConn, error) {
	var (
		lCmd *localcommand.LocalCommand
		err  error
	)

	var args = &sqlOption{}

	for _, setter := range opts {
		setter(args)
	}

	lCmd, err = startUSQLCommand(args)
	if err != nil {
		return nil, err
	}
	err = lCmd.SetWinSize(args.win.Width, args.win.Height)
	if err != nil {
		_ = lCmd.Close()
		return nil, err
	}
	return &USQLConn{options: args, LocalCommand: lCmd}, nil
}

type USQLConn struct {
	options *sqlOption
	*localcommand.LocalCommand
}

func (conn *USQLConn) KeepAlive() error { return nil }

func (conn *USQLConn) Close() error {
	_, _ = conn.Write(cleanLineExitCommand)
	return conn.LocalCommand.Close()
}

func startUSQLCommand(opt *sqlOption) (*localcommand.LocalCommand, error) {
	args, err := opt.USQLCommandArgs()
	if err != nil {
		return nil, err
	}
	lcmd, err := localcommand.New("usql", args, localcommand.WithEnv([]string{
		"PAGE=",
	}))
	if err != nil {
		return nil, err
	}
	return lcmd, nil
}

func (o *sqlOption) USQLCommandArgs() ([]string, error) {
	var dsnURL url.URL
	dsnURL.Scheme = o.Schema
	dsnURL.Host = net.JoinHostPort(o.Host, strconv.Itoa(o.Port))
	dsnURL.User = url.UserPassword(o.Username, o.Password)
	dsnURL.Path = o.DBName
	params := url.Values{}
	if o.UseSSL {
		clientCertKeyPath, err := StorePrivateKeyFileToLocal(o.CertKey)
		if err != nil {
			return nil, err
		}
		clientCertPath, err := StoreCAFileToLocal(o.ClientCert)
		if err != nil {
			return nil, err
		}
		caCertPath, err := StoreCAFileToLocal(o.CaCert)
		if err != nil {
			return nil, err
		}

		defer ClearTempFileDelay(time.Minute, clientCertPath, clientCertKeyPath, caCertPath)

		switch o.Schema {
		case "postgres":
			params.Set("sslmode", o.SqlPgSSLMode)

			if caCertPath != "" {
				params.Set("sslrootcert", caCertPath)
			}

			if clientCertPath != "" {
				params.Set("sslcert", clientCertPath)
			}

			if clientCertKeyPath != "" {
				params.Set("sslkey", clientCertKeyPath)
			}

		case "mysql":
			params.Set("tls", "custom")
			params.Set("ssl-ca", caCertPath)
			params.Set("ssl-cert", clientCertPath)
			params.Set("ssl-key", clientCertKeyPath)
		}
	}

	switch o.Schema {
	case "sqlserver":
		if o.SQLServerDisableEncrypt {
			params.Set("encrypt", "disable")
		}
	default:
	}
	dsnURL.RawQuery = params.Encode()
	dsn := dsnURL.String()
	prompt1 := "--variable=PROMPT1=" + o.Schema + "%R%#"

	return []string{dsn, prompt1}, nil
}
