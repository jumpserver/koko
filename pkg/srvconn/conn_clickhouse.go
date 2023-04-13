package srvconn

import (
	"fmt"
	"os"
	"strconv"

	_ "github.com/ClickHouse/clickhouse-go"
	"github.com/jumpserver/koko/pkg/localcommand"
)

const (
	clickhousePrompt = "Password for user"
)

var (
	_ ServerConnection = (*ClickHouseConn)(nil)
)

func NewClickHouseConnection(ops ...SqlOption) (*ClickHouseConn, error) {
	var (
		lCmd *localcommand.LocalCommand
		err  error
	)
	args := &sqlOption{
		Username:   os.Getenv("USER"),
		Password:   os.Getenv("PASSWORD"),
		Host:       "127.0.0.1",
		Port:       9000,
		DBName:     "default",
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

	if err := checkClickHouseAccount(args); err != nil {
		return nil, err
	}
	lCmd, err = startClickHouseCommand(args)

	if err != nil {
		return nil, err
	}
	err = lCmd.SetWinSize(args.win.Width, args.win.Height)
	if err != nil {
		_ = lCmd.Close()
		return nil, err
	}
	return &ClickHouseConn{options: args, LocalCommand: lCmd}, nil
}

type ClickHouseConn struct {
	options *sqlOption
	*localcommand.LocalCommand
}

func (conn *ClickHouseConn) KeepAlive() error {
	return nil
}

func (conn *ClickHouseConn) Close() error {
	_, _ = conn.Write(cleanLineExitCommand)
	return conn.LocalCommand.Close()
}

func startClickHouseCommand(opt *sqlOption) (lcmd *localcommand.LocalCommand, err error) {
	cmd := opt.ClickHouseCommandArgs()
	lcmd, err = localcommand.New("clickhouse-client", cmd, localcommand.WithPtyWin(opt.win.Width, opt.win.Height))
	if err != nil {
		return nil, err
	}
	if opt.Password != "" {
		lcmd, err = MatchLoginPrefix(clickhousePrompt, "ClickHouse", lcmd)
		if err != nil {
			return lcmd, err
		}
		lcmd, err = DoLogin(opt, lcmd, "ClickHouse")
		if err != nil {
			return lcmd, err
		}
	}
	return lcmd, nil
}

func (opt *sqlOption) ClickHouseCommandArgs() []string {
	params := []string{
		"-h", opt.Host, "--port", strconv.Itoa(opt.Port),
		"-u", opt.Username, "-d", opt.DBName, "--highlight", "off",
	}
	if opt.Password != "" {
		params = append(params, "--ask-password")
	}
	return params
}

func (opt *sqlOption) ClickHouseDataSourceName() string {
	return fmt.Sprintf("tcp://%s:%d/%s?username=%s&password=%s",
		opt.Host,
		opt.Port,
		opt.DBName,
		opt.Username,
		opt.Password,
	)
}

func checkClickHouseAccount(args *sqlOption) error {
	return checkDatabaseAccountValidate("clickhouse", args.ClickHouseDataSourceName())
}
