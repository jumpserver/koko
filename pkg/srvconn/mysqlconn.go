package srvconn

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/creack/pty"
	"github.com/jumpserver/koko/pkg/logger"
)

const (
	mysqlPrompt = "Enter password: "
)

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
}

func (dbconn *ServerMysqlConnection) Connect() (err error) {
	cmd := exec.Command("mysql", dbconn.options.CommandArgs()...)
	nobody, err := user.Lookup("nobody")
	if err != nil {
		logger.Errorf("lookup nobody user err: %s", err)
		return errors.New("nobody user does not exist")
	}

	cmd.SysProcAttr = &syscall.SysProcAttr{}
	uid, _ := strconv.Atoi(nobody.Uid)
	gid, _ := strconv.Atoi(nobody.Gid)
	cmd.SysProcAttr.Credential = &syscall.Credential{Uid: uint32(uid), Gid: uint32(gid)}
	fd, err := pty.Start(cmd)
	if err != nil {
		logger.Errorf("pty start err: %s", err)
		return fmt.Errorf("start local pty err: %s", err)
	}
	prompt := [20]byte{}
	nr, err := fd.Read(prompt[:])
	if err != nil {
		logger.Errorf("read mysql pty local fd err: %s", err)
		return fmt.Errorf("mysql conn err: %s", err)
	}
	if !bytes.Equal(prompt[:nr], []byte(mysqlPrompt)) {
		_ = fd.Close()
		logger.Errorf("mysql login prompt characters did not match: %s", prompt[:nr])
		return errors.New("failed login mysql")
	}
	// 输入密码, 登录mysql
	_, err = fd.Write([]byte(dbconn.options.Password + "\r\n"))
	if err != nil {
		return
	}
	dbconn.ptyFD = fd
	logger.Infof("Connect mysql database %s success ", dbconn.options.Host)
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
	return pty.Setsize(dbconn.ptyFD, &win)
}

func (dbconn *ServerMysqlConnection) Close() (err error) {
	dbconn.onceClose.Do(func() {
		if dbconn.ptyFD == nil {
			return
		}
		err = dbconn.ptyFD.Close()
	})
	return err
}

func (dbconn *ServerMysqlConnection) Timeout() time.Duration {
	return time.Duration(10) * time.Second
}

func (dbconn *ServerMysqlConnection) Protocol() string {
	return "mysql"
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
