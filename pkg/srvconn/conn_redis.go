package srvconn

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/jumpserver/koko/pkg/localcommand"
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
		Username: os.Getenv("USER"),
		Password: os.Getenv("PASSWORD"),
		Host:     "127.0.0.1",
		Port:     6379,
		DBName:   "0",
		win: Windows{
			Width:  80,
			Height: 120,
		},
	}
	for _, setter := range ops {
		setter(args)
	}

	if args.UseSSL {
		CaCertPath, err := StoreCAFileToLocal(args.CaCert)
		if err != nil {
			return nil, err
		}
		args.CaCertPath = CaCertPath
		defer ClearTempFileDelay(time.Minute, CaCertPath)
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
	_, _ = conn.Write([]byte("\r\nexit\r\n"))
	return conn.LocalCommand.Close()
}

func startRedisCommand(opt *sqlOption) (lcmd *localcommand.LocalCommand, err error) {
	cmd := opt.RedisCommandArgs()
	lcmd, err = localcommand.New("redis-cli", cmd, localcommand.WithPtyWin(opt.win.Width, opt.win.Height))
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
		params = append(params, "--insecure")
		params = append(params, "--cacert", opt.CaCertPath)
	}
	if opt.Username != "" {
		params = append(params, "--user", opt.Username)
	}
	if opt.Password != "" {
		params = append(params, "--askpass")
	}
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

	if args.UseSSL{
		rootCAs := x509.NewCertPool()
		rootCAs.AppendCertsFromPEM([]byte(args.CaCert))
		tlsConfig := tls.Config{
			InsecureSkipVerify:true,
			RootCAs: rootCAs,
		}
		dialOptions = append(dialOptions, radix.DialUseTLS(&tlsConfig))
	}

	conn, err := radix.Dial("tcp", addr, dialOptions...)
	if err != nil || conn == nil {
		return err
	}
	defer conn.Close()
	return nil
}
