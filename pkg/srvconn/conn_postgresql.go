package srvconn

import (
	"fmt"
	"os"
	"strconv"

	"github.com/jumpserver/koko/pkg/logger"
	_ "github.com/lib/pq"

	"github.com/jumpserver/koko/pkg/localcommand"
)

const (
	PostgreSQLPrompt = "Password for user %s:"
)

func NewPostgreSQLConnection(ops ...SqlOption) (*PostgreSQLConn, error) {
	args := &sqlOption{
		Username: os.Getenv("USER"),
		Password: os.Getenv("PASSWORD"),
		Host:     "127.0.0.1",
		Port:     5432,
		DBName:   "postgres",
		win: Windows{
			Width:  80,
			Height: 120,
		},
	}
	for _, setter := range ops {
		setter(args)
	}
	if err := checkPostgreSQLAccount(args); err != nil {
		return nil, err
	}
	lCmd, err := startPostgreSQLCommand(args)
	if err != nil {
		return nil, err
	}
	err = lCmd.SetWinSize(args.win.Width, args.win.Height)
	if err != nil {
		_ = lCmd.Close()
		return nil, err
	}
	return &PostgreSQLConn{options: args, LocalCommand: lCmd}, nil
}

type PostgreSQLConn struct {
	options *sqlOption
	*localcommand.LocalCommand
}

func (conn *PostgreSQLConn) KeepAlive() error {
	return nil
}

func (conn *PostgreSQLConn) Close() error {
	_, _ = conn.Write([]byte("\r\nexit\r\n"))
	return conn.LocalCommand.Close()
}

func startPostgreSQLCommand(opt *sqlOption) (lcmd *localcommand.LocalCommand, err error) {
	argv := opt.PostgreSQLCommandArgs()
	//psql 是启动postgresql的客户端
	opts, err := BuildNobodyWithOpts(localcommand.WithPtyWin(opt.win.Width, opt.win.Height))
	if err != nil {
		logger.Errorf("build nobody with opts error: %s", err)
		return nil, err
	}
	lcmd, err = localcommand.New("psql", argv, opts...)
	if err != nil {
		return nil, err
	}
	if opt.Password != "" {
		lcmd, err = MatchLoginPrefix(fmt.Sprintf(PostgreSQLPrompt, opt.Username), "PostgreSQL", lcmd)
		if err != nil {
			return lcmd, err
		}
		lcmd, err = DoLogin(opt, lcmd, "PostgreSQL")
		if err != nil {
			return lcmd, err
		}
	}
	return lcmd, nil
}

func (opt *sqlOption) PostgreSQLCommandArgs() []string {
	return []string{
		"-U", opt.Username,
		"-h", opt.Host,
		"-p", strconv.Itoa(opt.Port),
		"-d", opt.DBName,
	}
}

func (opt *sqlOption) PostgreSQLDataSourceName() string {
	return fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=disable",
		opt.Host,
		opt.Port,
		opt.Username,
		opt.Password,
		opt.DBName,
	)
}
func checkPostgreSQLAccount(args *sqlOption) error {
	return checkDatabaseAccountValidate("postgres", args.PostgreSQLDataSourceName())
}
