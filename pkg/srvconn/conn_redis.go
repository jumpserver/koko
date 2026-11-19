package srvconn

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/jumpserver/koko/pkg/localcommand"
	"github.com/jumpserver/koko/pkg/logger"
)

const (
	redisPrompt = "Enter password:"
)

var (
	_ ServerConnection = (*RedisConn)(nil)
)

func NewRedisConnection(ops ...SqlOption) (*RedisConn, error) {
	var (
		lCmd *localcommand.LocalCommand
		err  error
	)
	args := &sqlOption{
		Username:   os.Getenv("USER"),
		Password:   os.Getenv("PASSWORD"),
		Host:       "127.0.0.1",
		Port:       6379,
		DBName:     "0",
		UseSSL:     false,
		CaCert:     "",
		ClientCert: "",
		CertKey:    "",
		win: Windows{
			Width:  80,
			Height: 120,
		},
	}
	for _, setter := range ops {
		setter(args)
	}

	if args.UseSSL {
		caCertPath, err := StoreCAFileToLocal(args.CaCert)
		if err != nil {
			return nil, err
		}
		certKeyPath, err := StoreCAFileToLocal(args.CertKey)
		if err != nil {
			return nil, err
		}
		clientCertPath, err := StoreCAFileToLocal(args.ClientCert)
		if err != nil {
			return nil, err
		}
		args.CaCertPath = caCertPath
		args.CertKeyPath = certKeyPath
		args.ClientCertPath = clientCertPath
		defer ClearTempFileDelay(time.Minute, caCertPath, certKeyPath, clientCertPath)
	}

	if err := checkRedisAccount(args); err != nil {
		return nil, err
	}
	lCmd, err = startRedisCommand(args)

	if err != nil {
		return nil, err
	}
	err = lCmd.SetWinSize(args.win.Width, args.win.Height)
	if err != nil {
		_ = lCmd.Close()
		return nil, err
	}
	return &RedisConn{options: args, LocalCommand: lCmd}, nil
}

type RedisConn struct {
	options *sqlOption
	*localcommand.LocalCommand
}

func (conn *RedisConn) KeepAlive() error {
	return nil
}

func (conn *RedisConn) Close() error {
	return conn.LocalCommand.Close()
}

func startRedisCommand(opt *sqlOption) (lcmd *localcommand.LocalCommand, err error) {
	cmd := opt.RedisCommandArgs()
	envs := []string{"USQL_HISTORY=/dev/null"}
	ptyOpt := localcommand.WithPtyWin(opt.win.Width, opt.win.Height)
	envOpt := localcommand.WithEnv(envs)
	opts, err := BuildNobodyWithOpts(ptyOpt, envOpt)
	if err != nil {
		logger.Errorf("build nobody with opts error: %s", err)
		return nil, err
	}
	opts = append(opts, envOpt)
	lcmd, err = localcommand.New("usql", cmd, opts...)
	if err != nil {
		return nil, err
	}
	if opt.Password != "" {
		lcmd, err = MatchLoginPrefix(redisPrompt, "Redis", lcmd)
		if err != nil {
			return lcmd, err
		}
		lcmd, err = doNoSQLLogin(opt, lcmd)
		if err != nil {
			return lcmd, err
		}
	}
	return lcmd, nil
}

func (opt *sqlOption) RedisCommandArgs() []string {
	u := &url.URL{Scheme: "redis", Host: net.JoinHostPort(opt.Host, strconv.Itoa(opt.Port)), Path: "/" + opt.DBName}
	params := url.Values{}
	if opt.ClusterMode {
		params.Set("cluster", "true")
	}
	if opt.UseSSL {
		u.Scheme = "rediss"
		if opt.CaCertPath != "" {
			params.Set("tls-ca", opt.CaCertPath)
		}
		if opt.ClientCertPath != "" && opt.CertKeyPath != "" {
			params.Set("tls-cert", opt.ClientCertPath)
			params.Set("tls-key", opt.CertKeyPath)
		}
		if opt.ServerName != "" {
			params.Set("tls-server-name", opt.ServerName)
		}
		if opt.AllowInvalidCert {
			params.Set("tls-insecure", "true")
		}
	}
	u.RawQuery = params.Encode()
	return noSQLCommandArgs(u, opt)
}

func checkRedisAccount(args *sqlOption) error {
	addr := fmt.Sprintf("%s:%s", args.Host, strconv.Itoa(args.Port))
	var tlsConfig *tls.Config
	if args.UseSSL {
		config := tls.Config{}
		// 连接使用的是内部地址或者localhost时，跳过证书验证
		if args.Host == "127.0.0.1" || args.Host == "localhost" {
			config.InsecureSkipVerify = true
		}
		if args.CaCert != "" {
			rootCAs := x509.NewCertPool()
			rootCAs.AppendCertsFromPEM([]byte(args.CaCert))
			config.RootCAs = rootCAs
			config.InsecureSkipVerify = true
		}
		if args.CertKey != "" && args.ClientCert != "" {
			var err error
			config.Certificates = make([]tls.Certificate, 1)
			config.Certificates[0], err = tls.X509KeyPair([]byte(args.ClientCert), []byte(args.CertKey))
			if err != nil {
				return err
			}
		}
		tlsConfig = &config
	}

	client := redis.NewClient(&redis.Options{
		Addr:         addr,
		Username:     args.Username,
		Password:     args.Password,
		DialTimeout:  10 * time.Second,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		MaxRetries:   -1,
		TLSConfig:    tlsConfig,
	})
	defer client.Close()
	return client.Ping(context.Background()).Err()
}
