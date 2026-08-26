package srvconn

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/jumpserver/koko/pkg/localcommand"
	"github.com/jumpserver/koko/pkg/logger"
)

const (
	redisPrompt = "Please input password:"
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
	_, _ = conn.Write(cleanLineExitCommand)
	return conn.LocalCommand.Close()
}

func startRedisCommand(opt *sqlOption) (lcmd *localcommand.LocalCommand, err error) {
	cmd := opt.RedisCommandArgs()
	envs := make([]string, 0, 2)
	redisCliFile := os.Getenv("REDISCLI_RCFILE")
	if redisCliFile != "" {
		envs = append(envs, "REDISCLI_RCFILE="+redisCliFile)
		logger.Infof("rediscli rcfile: %s", redisCliFile)
	}
	ptyOpt := localcommand.WithPtyWin(opt.win.Width, opt.win.Height)
	envOpt := localcommand.WithEnv(envs)
	opts, err := BuildNobodyWithOpts(ptyOpt, envOpt)
	if err != nil {
		logger.Errorf("build nobody with opts error: %s", err)
		return nil, err
	}
	opts = append(opts, envOpt)
	lcmd, err = localcommand.New("redis-cli", cmd, opts...)
	if err != nil {
		return nil, err
	}
	if opt.Password != "" {
		lcmd, err = MatchLoginPrefix(redisPrompt, "Redis", lcmd)
		if err != nil {
			return lcmd, err
		}
		lcmd, err = DoLogin(opt, lcmd, "Redis")
		if err != nil {
			return lcmd, err
		}
	}
	return lcmd, nil
}

func (opt *sqlOption) RedisCommandArgs() []string {
	params := make([]string, 0, 15)
	if opt.ClusterMode {
		params = append(params, "-c")
	}
	connArgs := []string{
		"-h", opt.Host, "-p", strconv.Itoa(opt.Port),
		"-n", opt.DBName,
	}
	params = append(params, connArgs...)

	if opt.UseSSL {
		params = append(params, "--tls")
		if opt.CaCertPath != "" {
			params = append(params, "--cacert", opt.CaCertPath)
		}
		if opt.ClientCertPath != "" && opt.CertKeyPath != "" {
			params = append(params, "--cert", opt.ClientCertPath)
			params = append(params, "--key", opt.CertKeyPath)
		}
	}
	if opt.Username != "" {
		params = append(params, "--user", opt.Username)
	}
	if opt.Password != "" {
		params = append(params, "--askpass")
	}
	params = append(params, "--raw")
	return params
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
