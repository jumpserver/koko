package srvconn

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"strconv"
	"sync"
	"syscall"

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
exec su -s /bin/bash --command="mysql --user=${USERNAME} --host=${HOSTNAME} --port=${PORT} --password ${DATABASE}" nobody
`

var mysqlOnce sync.Once

func NewMySQLConnection(ops ...SqlOption) *MySQLConn {
	args := &sqlOption{
		Username: os.Getenv("USER"),
		Password: os.Getenv("PASSWORD"),
		Host:     "127.0.0.1",
		Port:     3306,
		DBName:   "",
	}
	for _, setter := range ops {
		setter(args)
	}
	return &MySQLConn{options: args}
}

type MySQLConn struct {
	options *sqlOption
	*localcommand.LocalCommand
}

func (conn *MySQLConn) Connect(win Windows) (err error) {
	lcmd, err := startMySQLCommand(conn)
	if err != nil {
		logger.Errorf("Start mysql command err: %s", err)
		return err
	}
	_ = lcmd.SetWinSize(win.Width, win.Height)
	conn.LocalCommand = lcmd
	logger.Infof("Connect mysql database %s success ", conn.options.Host)
	return
}

func (conn *MySQLConn) KeepAlive() error {
	return nil
}

func (conn *MySQLConn) Close() error {
	_, _ = conn.Write([]byte("exit\r\n"))
	return conn.LocalCommand.Close()
}

func startMySQLCommand(dbcon *MySQLConn) (lcmd *localcommand.LocalCommand, err error) {
	initOnceLinuxMySQLShellFile()
	if mysqlShellPath != "" {
		if lcmd, err = startMySQLNameSpaceCommand(dbcon.options); err == nil {
			if lcmd, err = tryManualLoginMySQLServer(dbcon, lcmd); err == nil {
				return lcmd, nil
			}
		}
	}
	if lcmd, err = startMySQLNormalCommand(dbcon.options); err != nil {
		return nil, err
	}
	return tryManualLoginMySQLServer(dbcon, lcmd)

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

func tryManualLoginMySQLServer(conn *MySQLConn, lcmd *localcommand.LocalCommand) (*localcommand.LocalCommand, error) {
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
	_, err = lcmd.Write([]byte(conn.options.Password + "\r\n"))
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
}

func (opt *sqlOption) CommandArgs() []string {
	return []string{
		fmt.Sprintf("--user=%s", opt.Username),
		fmt.Sprintf("--host=%s", opt.Host),
		fmt.Sprintf("--port=%d", opt.Port),
		"--password",
		opt.DBName,
	}
}

func (opt *sqlOption) Envs() []string {
	return []string{
		fmt.Sprintf("USERNAME=%s", opt.Username),
		fmt.Sprintf("HOSTNAME=%s", opt.Host),
		fmt.Sprintf("PORT=%d", opt.Port),
		fmt.Sprintf("DATABASE=%s", opt.DBName),
	}
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
