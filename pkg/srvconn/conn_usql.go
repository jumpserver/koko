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
	args, err := opt.USQLCommandArgs(opt)
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

func (o *sqlOption) USQLCommandArgs(opt *sqlOption) ([]string, error) {
	var dsnURL url.URL
	dsnURL.Scheme = o.Schema
	dsnURL.Host = net.JoinHostPort(o.Host, strconv.Itoa(o.Port))
	dsnURL.User = url.UserPassword(o.Username, o.Password)
	dsnURL.Path = o.DBName

	if opt.UseSSL {
		switch o.Schema {
		case "postgres":
			clientCertKeyPath, err := StoreCAFileToLocal(opt.CertKey)
			if err != nil {
				return nil, err
			}
			clientCertPath, err := StoreCAFileToLocal(opt.ClientCert)
			if err != nil {
				return nil, err
			}

			params := url.Values{}

			params.Set("sslcert", clientCertPath)
			params.Set("sslkey", clientCertKeyPath)

			if opt.CaCert != "" {
				caCertPath, err := StoreCAFileToLocal(opt.CaCert)
				if err != nil {
					return nil, err
				}
				params.Set("sslrootcert", caCertPath)

				params.Set("sslmode", "verify-full")
				defer ClearTempFileDelay(time.Minute, clientCertPath, clientCertKeyPath, caCertPath)
			} else {
				params.Set("sslmode", "require")
				defer ClearTempFileDelay(time.Minute, clientCertPath, clientCertKeyPath)
			}
			dsnURL.RawQuery = params.Encode()

		case "mysql":
			params := url.Values{}

			caCertPath, err := StoreCAFileToLocal(opt.CaCert)
			if err != nil {
				return nil, err
			}
			clientCertKeyPath, err := StoreCAFileToLocal(opt.CertKey)
			if err != nil {
				return nil, err
			}
			clientCertPath, err := StoreCAFileToLocal(opt.ClientCert)
			if err != nil {
				return nil, err
			}

			params.Set("tls", "custom")
			params.Set("ssl-ca", caCertPath)
			params.Set("ssl-cert", clientCertPath)
			params.Set("ssl-key", clientCertKeyPath)

			dsnURL.RawQuery = params.Encode()

			defer ClearTempFileDelay(time.Minute, caCertPath, clientCertPath, clientCertKeyPath)
		}
	}

	dsn := dsnURL.String()
	prompt1 := "--variable=PROMPT1=" + o.AssetName + "%R%#"

	return []string{dsn, prompt1}, nil
}
