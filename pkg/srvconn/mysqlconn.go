package srvconn

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/creack/pty"

	"github.com/jumpserver/koko/pkg/logger"
)

const (
	mysqlPrompt = "Enter password: "

	mysqlShellFilename = "mysql"
)

var mysqlShellPath = ""

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

func NewMysqlServer(ops ...SqlOption) *ServerMysqlConnection {
	args := &SqlOptions{
		Username: os.Getenv("USER"),
		Password: os.Getenv("PASSWORD"),
		Host:     "127.0.0.1",
		Port:     3306,
		DBName:   "",
	}
	for _, setter := range ops {
		setter(args)
	}
	return &ServerMysqlConnection{options: args, onceClose: new(sync.Once)}
}

type ServerMysqlConnection struct {
	options   *SqlOptions
	ptyFD     *os.File
	onceClose *sync.Once
	cmd       *exec.Cmd
}

func (dbconn *ServerMysqlConnection) Connect() (err error) {
	cmd, ptyFD, err := connectMysql(dbconn)
	go func() {
		err = cmd.Wait()
		if err != nil {
			logger.Errorf("mysql command exit err: %s", err)
		}
		if ptyFD != nil {
			_ = ptyFD.Close()
		}
		logger.Info("mysql connect closed.")
		var wstatus syscall.WaitStatus
		_, err = syscall.Wait4(-1, &wstatus, 0, nil)
	}()
	if err != nil {
		logger.Errorf("pty start err: %s", err)
		return fmt.Errorf("start local pty err: %s", err)
	}
	// 输入密码, 登录mysql
	_, err = ptyFD.Write([]byte(dbconn.options.Password + "\r\n"))
	if err != nil {
		_ = ptyFD.Close()
		_ = cmd.Process.Kill()
		logger.Errorf("mysql local pty write err: %s", err)
		return fmt.Errorf("mysql conn err: %s", err)
	}
	logger.Infof("Connect mysql database %s success ", dbconn.options.Host)
	dbconn.cmd = cmd
	dbconn.ptyFD = ptyFD
	return
}

func (dbconn *ServerMysqlConnection) Read(p []byte) (int, error) {
	if dbconn.ptyFD == nil {
		return 0, fmt.Errorf("not connect init")
	}
	return dbconn.ptyFD.Read(p)
}

func (dbconn *ServerMysqlConnection) Write(p []byte) (int, error) {
	if dbconn.ptyFD == nil {
		return 0, fmt.Errorf("not connect init")
	}
	return dbconn.ptyFD.Write(p)
}

func (dbconn *ServerMysqlConnection) SetWinSize(w, h int) error {
	if dbconn.ptyFD == nil {
		return fmt.Errorf("not connect init")
	}
	win := pty.Winsize{
		Rows: uint16(h),
		Cols: uint16(w),
	}
	logger.Infof("db conn windows size change %d*%d", h, w)
	return pty.Setsize(dbconn.ptyFD, &win)
}

func (dbconn *ServerMysqlConnection) Close() (err error) {
	dbconn.onceClose.Do(func() {
		if dbconn.ptyFD == nil {
			return
		}
		_ = dbconn.ptyFD.Close()
		err = dbconn.cmd.Process.Signal(os.Kill)
	})
	return
}

func (dbconn *ServerMysqlConnection) Timeout() time.Duration {
	return time.Duration(10) * time.Second
}

func (dbconn *ServerMysqlConnection) Protocol() string {
	return "mysql"
}

func connectMysql(dbconn *ServerMysqlConnection) (cmd *exec.Cmd, ptyFD *os.File, err error) {
	mysqlOnce.Do(func() {
		// linux初始化mysql命令文件
		switch runtime.GOOS {
		case "linux":
			initMySQLFile()
		}
	})
	if mysqlShellPath != "" {
		cmd, ptyFD, err = connectMySQLWithNamespace(dbconn.options.Envs())
		if err == nil {
			return
		}
	}
	return connectMySQLWithNormal(dbconn.options.CommandArgs())
}

func connectMySQLWithNamespace(envs []string) (cmd *exec.Cmd, ptyFD *os.File, err error) {
	args := []string{
		"--fork",
		"--pid",
		"--mount-proc",
		mysqlShellPath,
	}
	cmd = exec.Command("unshare", args...)
	cmd.Env = envs
	ptyFD, err = pty.Start(cmd)
	if err == nil {
		var nr int
		prompt := [len(mysqlPrompt)]byte{}
		nr, err = ptyFD.Read(prompt[:])
		if err != nil {
			_ = ptyFD.Close()
			_ = cmd.Process.Kill()
			logger.Errorf("read mysql pty local fd err: %s", err)
			return

		}
		if !bytes.Equal(prompt[:nr], []byte(mysqlPrompt)) {
			_ = cmd.Process.Kill()
			_ = ptyFD.Close()
			logger.Errorf("mysql login prompt characters did not match: %s", prompt[:nr])
			err = fmt.Errorf("mysql login prompt characters did not match: %s", prompt[:nr])
			return
		}
	}
	return
}

func connectMySQLWithNormal(args []string) (cmd *exec.Cmd, ptyFD *os.File, err error) {
	cmd = exec.Command("mysql", args...)
	nobody, err := user.Lookup("nobody")
	if err != nil {
		logger.Errorf("lookup nobody user err: %s", err)
		return
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{}
	uid, _ := strconv.Atoi(nobody.Uid)
	gid, _ := strconv.Atoi(nobody.Gid)
	cmd.SysProcAttr.Credential = &syscall.Credential{Uid: uint32(uid), Gid: uint32(gid)}
	ptyFD, err = pty.Start(cmd)
	if err == nil {
		var nr int
		prompt := [len(mysqlPrompt)]byte{}
		nr, err = ptyFD.Read(prompt[:])
		if err != nil {
			_ = ptyFD.Close()
			_ = cmd.Process.Kill()
			logger.Errorf("read mysql pty local fd err: %s", err)
			return

		}
		if !bytes.Equal(prompt[:nr], []byte(mysqlPrompt)) {
			_ = cmd.Process.Kill()
			_ = ptyFD.Close()
			logger.Errorf("mysql login prompt characters did not match: %s", prompt[:nr])
			err = fmt.Errorf("mysql login prompt characters did not match: %s", prompt[:nr])
			return
		}
	}
	return
}

func initMySQLFile() {
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

type SqlOptions struct {
	Username string
	Password string
	DBName   string
	Host     string
	Port     int
}

func (opts *SqlOptions) CommandArgs() []string {
	return []string{
		fmt.Sprintf("--user=%s", opts.Username),
		fmt.Sprintf("--host=%s", opts.Host),
		fmt.Sprintf("--port=%d", opts.Port),
		"--password",
		opts.DBName,
	}
}

func (opts *SqlOptions) Envs() []string {
	return []string{
		fmt.Sprintf("USERNAME=%s", opts.Username),
		fmt.Sprintf("HOSTNAME=%s", opts.Host),
		fmt.Sprintf("PORT=%d", opts.Port),
		fmt.Sprintf("DATABASE=%s", opts.DBName),
	}
}

type SqlOption func(*SqlOptions)

func SqlUsername(username string) SqlOption {
	return func(args *SqlOptions) {
		args.Username = username
	}
}

func SqlPassword(password string) SqlOption {
	return func(args *SqlOptions) {
		args.Password = password
	}
}

func SqlDBName(dbName string) SqlOption {
	return func(args *SqlOptions) {
		args.DBName = dbName
	}
}

func SqlHost(host string) SqlOption {
	return func(args *SqlOptions) {
		args.Host = host
	}
}

func SqlPort(port int) SqlOption {
	return func(args *SqlOptions) {
		args.Port = port
	}
}
