package srvconn

import (
	"bytes"
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
		lcmd, err = matchLoginPrefix(redisPrompt, lcmd)
		if err != nil {
			return lcmd, err
		}
		lcmd, err = doLogin(opt, lcmd)
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
	conn, err := radix.Dial("tcp", addr, dialOptions...)
	if err != nil || conn == nil {
		return err
	}
	defer conn.Close()
	return nil
}

func matchLoginPrefix(prefix string, lcmd *localcommand.LocalCommand) (*localcommand.LocalCommand, error) {
	var (
		nr  int
		err error
	)
	prompt := make([]byte, len(prefix))
	nr, err = lcmd.Read(prompt[:])
	if err != nil {
		_ = lcmd.Close()
		logger.Errorf("redis local pty fd read err: %s", err)
		return lcmd, err
	}
	if !bytes.Equal(prompt[:nr], []byte(prefix)) {
		_ = lcmd.Close()
		logger.Errorf("redis login prompt characters did not match: %s", prompt[:nr])
		err = fmt.Errorf("redis login prompt characters did not match: %s", prompt[:nr])
		return lcmd, err
	}
	return lcmd, nil
}

func doLogin(opt *sqlOption, lcmd *localcommand.LocalCommand) (*localcommand.LocalCommand, error) {
	//输入密码, 登录 redis
	_, err := lcmd.Write([]byte(opt.Password + "\r\n"))
	if err != nil {
		_ = lcmd.Close()
		logger.Errorf("Redis local pty write err: %s", err)
		return lcmd, fmt.Errorf("redis conn err: %s", err)
	}
	// 清除掉输入密码后，界面上显示的星号
	time.Sleep(time.Millisecond * 100)
	clearPassword := make([]byte, len(opt.Password)+2)
	_, _ = lcmd.Read(clearPassword)
	return lcmd, nil
}
