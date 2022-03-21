package srvconn

import (
	"bytes"
	"database/sql"
	"fmt"
	"io/ioutil"
	"net"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"strconv"
	"sync"
	"syscall"
	"time"

	_ "github.com/go-sql-driver/mysql"

	"github.com/jumpserver/koko/pkg/localcommand"
	"github.com/jumpserver/koko/pkg/logger"
)

const (
	mysqlPrompt = "Enter password: "

	mysqlShellFilename = "mysql"
)

var (
	mysqlShellPath = ""

	_ ServerConnection = (*MySQLConn)(nil)
)

const mysqlTemplate = `#!/bin/bash
set -e
mkdir -p /nonexistent
mount -t tmpfs -o size=10M tmpfs /nonexistent
cd /nonexistent
export HOME=/nonexistent
export TMPDIR=/nonexistent
export LANG=en_US.UTF-8
export TERM=xterm
exec su -s /bin/bash --command="mysql ${EXTRAARGS} --user=${USERNAME} --host=${HOSTNAME} --port=${PORT} --password ${DATABASE}" nobody
`

var mysqlOnce sync.Once

func NewMySQLConnection(ops ...SqlOption) (*MySQLConn, error) {
	args := &sqlOption{
		Username: os.Getenv("USER"),
		Password: os.Getenv("PASSWORD"),
		Host:     "127.0.0.1",
		Port:     3306,
		DBName:   "",
		win: Windows{
			Width:  80,
			Height: 120,
		},
	}
	for _, setter := range ops {
		setter(args)
	}
	if err := checkMySQLAccount(args); err != nil {
		return nil, err
	}
	lCmd, err := startMySQLCommand(args)
	if err != nil {
		return nil, err
	}
	err = lCmd.SetWinSize(args.win.Width, args.win.Height)
	if err != nil {
		_ = lCmd.Close()
		return nil, err
	}
	return &MySQLConn{options: args, LocalCommand: lCmd}, nil
}

type MySQLConn struct {
	options *sqlOption
	*localcommand.LocalCommand
}

func (conn *MySQLConn) KeepAlive() error {
	return nil
}

func (conn *MySQLConn) Close() error {
	_, _ = conn.Write([]byte("\r\nexit\r\n"))
	return conn.LocalCommand.Close()
}

func startMySQLCommand(opt *sqlOption) (lcmd *localcommand.LocalCommand, err error) {
	initOnceLinuxMySQLShellFile()
	if mysqlShellPath != "" {
		if lcmd, err = startMySQLNameSpaceCommand(opt); err == nil {
			if lcmd, err = tryManualLoginMySQLServer(opt, lcmd); err == nil {
				return lcmd, nil
			}
		}
	}
	if lcmd, err = startMySQLNormalCommand(opt); err != nil {
		return nil, err
	}
	return tryManualLoginMySQLServer(opt, lcmd)
}

func startMySQLNameSpaceCommand(opt *sqlOption) (*localcommand.LocalCommand, error) {
	argv := []string{
		"--fork",
		"--pid",
		"--mount-proc",
		mysqlShellPath,
	}
	return localcommand.New("unshare", argv, localcommand.WithEnv(opt.Envs()))
}

func startMySQLNormalCommand(opt *sqlOption) (*localcommand.LocalCommand, error) {
	// 使用 nobody 用户的权限
	nobody, err := user.Lookup("nobody")
	if err != nil {
		logger.Errorf("lookup nobody user err: %s", err)
		return nil, err
	}
	uid, _ := strconv.Atoi(nobody.Uid)
	gid, _ := strconv.Atoi(nobody.Gid)

	return localcommand.New("mysql", opt.CommandArgs(), localcommand.WithEnv(opt.Envs()),
		localcommand.WithCmdCredential(&syscall.Credential{Uid: uint32(uid), Gid: uint32(gid)}))
}

func tryManualLoginMySQLServer(opt *sqlOption, lcmd *localcommand.LocalCommand) (*localcommand.LocalCommand, error) {
	var (
		nr  int
		err error
	)
	prompt := [len(mysqlPrompt)]byte{}
	nr, err = lcmd.Read(prompt[:])
	if err != nil {
		_ = lcmd.Close()
		logger.Errorf("Mysql local pty fd read err: %s", err)
		return lcmd, err

	}
	if !bytes.Equal(prompt[:nr], []byte(mysqlPrompt)) {
		_ = lcmd.Close()
		logger.Errorf("Mysql login prompt characters did not match: %s", prompt[:nr])
		err = fmt.Errorf("mysql login prompt characters did not match: %s", prompt[:nr])
		return lcmd, err
	}

	// 输入密码, 登录 MySQL
	_, err = lcmd.Write([]byte(opt.Password + "\r\n"))
	if err != nil {
		_ = lcmd.Close()
		logger.Errorf("Mysql local pty write err: %s", err)
		return lcmd, fmt.Errorf("mysql conn err: %s", err)
	}
	return lcmd, nil
}

func initOnceLinuxMySQLShellFile() {
	mysqlOnce.Do(func() {
		// Linux系统 初始化 MySQL 命令文件
		switch runtime.GOOS {
		case "linux":
			if dir, err := os.Getwd(); err == nil {
				TmpMysqlShellPath := filepath.Join(dir, mysqlShellFilename)
				if _, err := os.Stat(TmpMysqlShellPath); err == nil {
					mysqlShellPath = TmpMysqlShellPath
					logger.Infof("Already init MySQL bash file: %s", TmpMysqlShellPath)
					return
				}
				err = ioutil.WriteFile(TmpMysqlShellPath, []byte(mysqlTemplate), os.FileMode(0755))
				if err != nil {
					logger.Errorf("Init MySQL bash file failed: %s", err)
					return
				}
				mysqlShellPath = TmpMysqlShellPath
			}
			logger.Infof("Init MySQL bash file: %s", mysqlShellPath)
		}
	})
}

type sqlOption struct {
	Username string
	Password string
	DBName   string
	Host     string
	Port     int

	win                    Windows
	disableMySQLAutoRehash bool
}

func (opt *sqlOption) CommandArgs() []string {
	args := make([]string, 0, 6)
	authRehashFlag := "--auto-rehash"
	if opt.disableMySQLAutoRehash {
		authRehashFlag = "--no-auto-rehash"
	}
	args = append(args, authRehashFlag)
	args = append(args, fmt.Sprintf("--user=%s", opt.Username))
	args = append(args, fmt.Sprintf("--host=%s", opt.Host))
	args = append(args, fmt.Sprintf("--port=%d", opt.Port))
	args = append(args, "--password")
	args = append(args, opt.DBName)
	return args
}

func (opt *sqlOption) Envs() []string {
	extraArgs := "--auto-rehash"
	if opt.disableMySQLAutoRehash {
		extraArgs = "--no-auto-rehash"
	}
	envs := make([]string, 0, 6)
	envs = append(envs, fmt.Sprintf("USERNAME=%s", opt.Username))
	envs = append(envs, fmt.Sprintf("HOSTNAME=%s", opt.Host))
	envs = append(envs, fmt.Sprintf("PORT=%d", opt.Port))
	envs = append(envs, fmt.Sprintf("DATABASE=%s", opt.DBName))
	envs = append(envs, fmt.Sprintf("EXTRAARGS=%s", extraArgs))
	return envs
}

func (opt *sqlOption) DataSourceName() string {
	// "user:password@tcp(127.0.0.1:3306)/hello"
	addr := net.JoinHostPort(opt.Host, strconv.Itoa(opt.Port))
	return fmt.Sprintf("%s:%s@tcp(%s)/%s",
		opt.Username,
		opt.Password,
		addr,
		opt.DBName,
	)
}

type SqlOption func(*sqlOption)

func SqlUsername(username string) SqlOption {
	return func(args *sqlOption) {
		args.Username = username
	}
}

func SqlPassword(password string) SqlOption {
	return func(args *sqlOption) {
		args.Password = password
	}
}

func SqlDBName(dbName string) SqlOption {
	return func(args *sqlOption) {
		args.DBName = dbName
	}
}

func SqlHost(host string) SqlOption {
	return func(args *sqlOption) {
		args.Host = host
	}
}

func SqlPort(port int) SqlOption {
	return func(args *sqlOption) {
		args.Port = port
	}
}

func SqlPtyWin(win Windows) SqlOption {
	return func(args *sqlOption) {
		args.win = win
	}
}

func MySQLDisableAutoReHash() SqlOption {
	return func(args *sqlOption) {
		args.disableMySQLAutoRehash = true
	}
}

const (
	mySQLMaxConnCount = 1
	mySQLMaxIdleTime  = time.Second * 15
)

func checkMySQLAccount(args *sqlOption) error {
	return checkDatabaseAccountValidate("mysql", args.DataSourceName())
}

func checkDatabaseAccountValidate(driveName, datasourceName string) error {
	db, err := sql.Open(driveName, datasourceName)
	if err != nil {
		return err
	}
	db.SetMaxOpenConns(mySQLMaxConnCount)
	db.SetMaxIdleConns(mySQLMaxConnCount)
	db.SetConnMaxLifetime(mySQLMaxIdleTime)
	db.SetConnMaxIdleTime(mySQLMaxIdleTime)
	defer db.Close()
	err = db.Ping()
	if err != nil {
		return err
	}
	return nil
}
