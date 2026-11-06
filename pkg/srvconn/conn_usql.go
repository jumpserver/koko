package srvconn

import (
	"encoding/json"
	"errors"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/jumpserver-dev/sdk-go/model"
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
		args.cleanupTLSFiles()
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
	// Input may already be blocked; close the process without another PTY write.
	err := conn.LocalCommand.Close()
	if conn.options != nil {
		conn.options.cleanupTLSFiles()
	}
	return err
}

func (o *sqlOption) cleanupTLSFiles() {
	if o.tlsDir != "" {
		// tlsDir is assigned only by MkdirTemp, never from an asset or DSN.
		_ = os.RemoveAll(o.tlsDir)
	}
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
		opt.cleanupTLSFiles()
		return nil, err
	}
	return lcmd, nil
}

func (o *sqlOption) USQLCommandArgs() ([]string, error) {
	ready := false
	defer func() {
		if !ready {
			o.cleanupTLSFiles()
		}
	}()
	if o.Schema == "db2" && strings.TrimSpace(o.DBName) == "" {
		return nil, errors.New("Db2 requires a database name; configure the asset's default database")
	}
	var dsnURL url.URL
	dsnURL.Scheme = o.Schema
	dsnURL.Host = net.JoinHostPort(o.Host, strconv.Itoa(o.Port))
	dsnURL.User = url.UserPassword(o.Username, o.Password)
	dsnURL.Path = o.DBName
	params := url.Values{}
	if o.Schema == "postgres" {
		params.Set("sslmode", "disable")
		params.Set("connect_timeout", "10")
	}
	if o.Schema == "mysql" || o.Schema == "maria" || o.Schema == "mariadb" {
		params.Set("tls", "false")
	}
	if o.UseSSL {
		if (o.Schema == "postgres" || o.Schema == "mysql" || o.Schema == "maria" || o.Schema == "mariadb") &&
			(o.ClientCert == "") != (o.CertKey == "") {
			return nil, errors.New("database client certificate and key must be configured together")
		}
		dir, err := os.MkdirTemp("", "koko-usql-tls-")
		if err != nil {
			return nil, err
		}
		o.tlsDir = dir
		paths := make([]string, 3)
		for i, content := range []string{o.CaCert, o.ClientCert, o.CertKey} {
			if content == "" {
				continue
			}
			paths[i] = filepath.Join(dir, strconv.Itoa(i)+".pem")
			if err := os.WriteFile(paths[i], []byte(content), 0600); err != nil {
				return nil, err
			}
		}
		caCertPath, clientCertPath, clientCertKeyPath := paths[0], paths[1], paths[2]

		switch o.Schema {
		case "db2":
			params.Set("ssl", "true")
			if o.ServerName != "" {
				params.Set("ssl-server-name", o.ServerName)
			}
			if caCertPath != "" {
				params.Set("ssl-ca", caCertPath)
			}
			if clientCertPath != "" {
				params.Set("ssl-cert", clientCertPath)
			}
			if clientCertKeyPath != "" {
				params.Set("ssl-key", clientCertKeyPath)
			}
		case "postgres":
			mode := o.SqlPgSSLMode
			if mode == "" {
				mode = "verify-full"
			}
			switch mode {
			case "prefer", "require", "verify-ca", "verify-full":
			default:
				return nil, errors.New("unsupported PostgreSQL SSL mode")
			}
			params.Set("sslmode", mode)
			if o.ServerName != "" && o.ServerName != o.Host {
				params.Set("hostaddr", o.Host)
				dsnURL.Host = net.JoinHostPort(o.ServerName, strconv.Itoa(o.Port))
			}

			if caCertPath != "" {
				params.Set("sslrootcert", caCertPath)
			}

			if clientCertPath != "" {
				params.Set("sslcert", clientCertPath)
			}

			if clientCertKeyPath != "" {
				params.Set("sslkey", clientCertKeyPath)
			}

		case "mysql", "maria", "mariadb", "oceanbase", "oboracle":
			params.Set("tls", "custom")
			params.Set("ssl-ca", caCertPath)
			params.Set("ssl-cert", clientCertPath)
			params.Set("ssl-key", clientCertKeyPath)
			params.Set("ssl-server-name", o.ServerName)
			params.Set("ssl-insecure", strconv.FormatBool(o.AllowInvalidCert))
		}
	}

	if len(o.DataMaskingRules) > 0 {
		rules := model.DataMaskingRules(o.DataMaskingRules)
		sort.Sort(rules)
		var activeRules []model.DataMaskingRule
		for _, rule := range rules {
			if rule.IsActive {
				activeRules = append(activeRules, rule)
			}
		}
		rulesJson, err := json.Marshal(activeRules)
		if err != nil {
			return nil, err
		}
		params.Set("data-masking-rules", string(rulesJson))
	}

	switch o.Schema {
	case "sqlserver":
		if o.SQLServerDisableEncrypt {
			params.Set("encrypt", "disable")
		}
	case "oracle":
		if o.OracleUseSYSDBA {
			params.Set("DBA PRIVILEGE", "SYSDBA")
		}
	default:
	}
	dsnURL.RawQuery = params.Encode()
	dsn := dsnURL.String()
	prompt1 := "--variable=PROMPT1=" + o.Schema + "%R%#"

	// KoKo collects the account password before launching usql. Let a failed
	// supplied password end this connection instead of usql recursively opening
	// an unlimited independent password prompt.
	ready = true
	return []string{dsn, prompt1, "--no-password"}, nil
}
