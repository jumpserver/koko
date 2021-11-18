package srvconn

import (
	"bytes"
	"fmt"
	"os"
	"strconv"

	_ "github.com/denisenkom/go-mssqldb"
	"github.com/jumpserver/koko/pkg/localcommand"
	"github.com/jumpserver/koko/pkg/logger"
)

const (
	sqlServerPrompt = "Password:"
)

var (
	_ ServerConnection = (*SQLServerConn)(nil)
)

func NewSQLServerConnection(ops ...SqlOption) (*SQLServerConn, error) {
	args := &sqlOption{
		Username: os.Getenv("USER"),
		Password: os.Getenv("PASSWORD"),
		Host:     "127.0.0.1",
		Port:     1433,
		DBName:   "",
		win: Windows{
			Width:  80,
			Height: 120,
		},
	}
	for _, setter := range ops {
		setter(args)
	}
	if err := checkSQLServerAccount(args); err != nil {
		return nil, err
	}
	lCmd, err := startSQLServerCommand(args)
	if err != nil {
		return nil, err
	}
	err = lCmd.SetWinSize(args.win.Width, args.win.Height)
	if err != nil {
		_ = lCmd.Close()
		return nil, err
	}
	return &SQLServerConn{options: args, LocalCommand: lCmd}, nil
}

type SQLServerConn struct {
	options *sqlOption
	*localcommand.LocalCommand
}

func (conn *SQLServerConn) KeepAlive() error {
	return nil
}

func (conn *SQLServerConn) Close() error {
	_, _ = conn.Write([]byte("\r\nexit\r\n"))
	return conn.LocalCommand.Close()
}

func startSQLServerCommand(opt *sqlOption) (lcmd *localcommand.LocalCommand, err error) {
	if lcmd, err = startSQLServerNormalCommand(opt); err != nil {
		return nil, err
	}
	return tryManualLoginSQLServerServer(opt, lcmd)
}

func startSQLServerNormalCommand(opt *sqlOption) (lcmd *localcommand.LocalCommand, err error) {
	//tsql 是启动sqlserver的客户端
	return localcommand.New("tsql", opt.SQLServerCommandArgs())
}

func tryManualLoginSQLServerServer(opt *sqlOption, lcmd *localcommand.LocalCommand) (*localcommand.LocalCommand, error) {
	var (
		nr  int
		err error
	)
	prompt := [len(sqlServerPrompt)]byte{}
	nr, err = lcmd.Read(prompt[:])
	if err != nil {
		_ = lcmd.Close()
		logger.Errorf("sqlserver local pty fd read err: %s", err)
		return lcmd, err
	}
	if !bytes.Equal(prompt[:nr], []byte(sqlServerPrompt)) {
		_ = lcmd.Close()
		logger.Errorf("sqlserver login prompt characters did not match: %s", prompt[:nr])
		err = fmt.Errorf("sqlserver login prompt characters did not match: %s", prompt[:nr])
		return lcmd, err
	}
	// 输入密码, 登录 sqlserver
	_, err = lcmd.Write([]byte(opt.Password + "\r\n"))
	if err != nil {
		_ = lcmd.Close()
		logger.Errorf("sqlserver local pty write err: %s", err)
		return lcmd, fmt.Errorf("sqlserver conn err: %s", err)
	}
	return lcmd, nil
}

func (opt *sqlOption) SQLServerCommandArgs() []string {
	return []string{
		"-U", opt.Username,
		"-S", opt.Host,
		"-p", strconv.Itoa(opt.Port),
		"-J", "UTF-8",
		"-D", opt.DBName,
	}
}

func (opt *sqlOption) SQLServerSourceName() string {
	return fmt.Sprintf("server=%s;port=%s;database=%s;user id=%s;password=%s",
		opt.Host,
		strconv.Itoa(opt.Port),
		opt.DBName,
		opt.Username,
		opt.Password,
	)
}

func checkSQLServerAccount(args *sqlOption) error {
	return checkDatabaseAccountValidate("mssql", args.SQLServerSourceName())
}
