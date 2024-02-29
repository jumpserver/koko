package srvconn

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/jumpserver/koko/pkg/localcommand"
	"github.com/jumpserver/koko/pkg/logger"
	"github.com/mediocregopher/radix/v3"
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
	opts, err := BuildNobodyWithOpts(localcommand.WithPtyWin(opt.win.Width, opt.win.Height))
	if err != nil {
		logger.Errorf("build nobody with opts error: %s", err)
		return nil, err
	}
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
	params := []string{
		"-h", opt.Host, "-p", strconv.Itoa(opt.Port),
		"-n", opt.DBName,
	}
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
	var dialOptions []radix.DialOpt
	addr := fmt.Sprintf("%s:%s", args.Host, strconv.Itoa(args.Port))
	if args.Username != "" {
		dialOptions = append(dialOptions, radix.DialAuthUser(args.Username, args.Password))
	} else {
		dialOptions = append(dialOptions, radix.DialAuthPass(args.Password))
	}

	if args.UseSSL {
		tlsConfig := tls.Config{}
		// 连接使用的是内部地址或者localhost时，跳过证书验证
		if args.Host == "127.0.0.1" || args.Host == "localhost" {
			tlsConfig.InsecureSkipVerify = true
		}
		if args.CaCert != "" {
			rootCAs := x509.NewCertPool()
			rootCAs.AppendCertsFromPEM([]byte(args.CaCert))
			tlsConfig.RootCAs = rootCAs
			tlsConfig.InsecureSkipVerify = true
		}
		if args.CertKey != "" && args.ClientCert != "" {
			var err error
			tlsConfig.Certificates = make([]tls.Certificate, 1)
			tlsConfig.Certificates[0], err = tls.X509KeyPair([]byte(args.ClientCert), []byte(args.CertKey))
			if err != nil {
				return err
			}
		}
		dialOptions = append(dialOptions, radix.DialUseTLS(&tlsConfig))
	}

	conn, err := radix.Dial("tcp", addr, dialOptions...)
	if err != nil || conn == nil {
		return err
	}
	defer conn.Close()
	err = conn.Do(radix.Cmd(nil, "PING"))
	return err
}
