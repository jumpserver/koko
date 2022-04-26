package srvconn

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"time"

	"github.com/jumpserver/koko/pkg/localcommand"
	"github.com/jumpserver/koko/pkg/logger"
)

type ServerConnection interface {
	io.ReadWriteCloser
	SetWinSize(width, height int) error
	KeepAlive() error
}

type Windows struct {
	Width  int
	Height int
}

const (
	ProtocolSSH    = "ssh"
	ProtocolTELNET = "telnet"
	ProtocolK8s    = "k8s"

	ProtocolMySQL     = "mysql"
	ProtocolMariadb   = "mariadb"
	ProtocolSQLServer = "sqlserver"
	ProtocolRedis     = "redis"
	ProtocolMongoDB   = "mongodb"
	ProtocolPostgreSQL   = "postgresql"
)

var (
	ErrUnSupportedProtocol = errors.New("unsupported protocol")

	ErrKubectlClient = errors.New("not found Kubectl client")

	ErrMySQLClient     = errors.New("not found MySQL client")
	ErrSQLServerClient = errors.New("not found SQLServer client")

	ErrRedisClient   = errors.New("not found Redis client")
	ErrMongoDBClient = errors.New("not found MongoDB client")
	ErrPostgreSQLClient = errors.New("not found PostgreSQL client")
)

type supportedChecker func() error

var supportedMap = map[string]supportedChecker{
	ProtocolSSH:       builtinSupported,
	ProtocolTELNET:    builtinSupported,
	ProtocolK8s:       kubectlSupported,
	ProtocolMySQL:     mySQLSupported,
	ProtocolMariadb:   mySQLSupported,
	ProtocolSQLServer: sqlServerSupported,
	ProtocolRedis:     redisSupported,
	ProtocolMongoDB:   mongoDBSupported,
	ProtocolPostgreSQL:   postgreSQLSupported,
}

func IsSupportedProtocol(p string) error {
	if checker, ok := supportedMap[p]; ok {
		return checker()
	}
	return ErrUnSupportedProtocol
}

func builtinSupported() error {
	return nil
}

func kubectlSupported() error {
	checkLine := "kubectl version --client -o json"
	cmd := exec.Command("bash", "-c", checkLine)
	out, err := cmd.CombinedOutput()
	if err != nil && len(out) == 0 {
		return fmt.Errorf("%w: %s", ErrKubectlClient, err)
	}
	var result map[string]interface{}
	err = json.Unmarshal(out, &result)
	if err != nil {
		return fmt.Errorf("%w: %s", ErrKubectlClient, err)
	}
	if _, ok := result["clientVersion"]; ok {
		return nil
	}
	return ErrKubectlClient
}

func mySQLSupported() error {
	checkLine := "mysql -V"
	cmd := exec.Command("bash", "-c", checkLine)
	out, err := cmd.CombinedOutput()
	if err != nil && len(out) == 0 {
		return fmt.Errorf("%w: %s", ErrMySQLClient, err)
	}
	if bytes.HasPrefix(out, []byte("mysql")) {
		return nil
	}
	return ErrMySQLClient
}

func redisSupported() error {
	checkLine := "redis-cli -v"
	cmd := exec.Command("bash", "-c", checkLine)
	out, err := cmd.CombinedOutput()
	if err != nil && len(out) == 0 {
		return fmt.Errorf("%w: %s", ErrRedisClient, err)
	}
	if bytes.HasPrefix(out, []byte("redis-cli")) {
		return nil
	}
	return ErrRedisClient
}

func mongoDBSupported() error {
	checkLine := "mongosh --version"
	cmd := exec.Command("bash", "-c", checkLine)
	out, err := cmd.CombinedOutput()
	if err != nil && len(out) == 0 {
		return fmt.Errorf("%w: %s", ErrRedisClient, err)
	}
	if !bytes.HasSuffix(out, []byte("command not found")) {
		return nil
	}
	return ErrRedisClient
}

func sqlServerSupported() error {
	checkLine := "tsql -C"
	cmd := exec.Command("bash", "-c", checkLine)
	out, err := cmd.CombinedOutput()
	if err != nil && len(out) == 0 {
		return fmt.Errorf("%w: %s", ErrSQLServerClient, err)
	}
	if strings.Contains(string(out), "freetds") {
		return nil
	}
	return ErrSQLServerClient
}

func postgreSQLSupported() error {
	checkLine := "psql -V"
	cmd := exec.Command("bash", "-c", checkLine)
	out, err := cmd.CombinedOutput()
	if err != nil && len(out) == 0 {
		return fmt.Errorf("%w: %s", ErrSQLServerClient, err)
	}
	if bytes.HasPrefix(out, []byte("psql")) {
		return nil
	}
	return ErrPostgreSQLClient
}

func MatchLoginPrefix(prefix string, dbType string, lcmd *localcommand.LocalCommand) (*localcommand.LocalCommand, error) {
	var (
		nr  int
		err error
	)
	prompt := make([]byte, len(prefix))
	var buf strings.Builder
	ctx, cancel := context.WithTimeout(context.TODO(), time.Minute)
	defer cancel()
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = lcmd.Close()
			logger.Errorf("%s login prompt characters matched timeout and closed", dbType)
			return
		case <-done:
			return
		}
	}()

	for {
		nr, err = lcmd.Read(prompt[:])
		if err != nil {
			_ = lcmd.Close()
			logger.Errorf("%s login prompt characters did not match: %s", dbType, buf.String())
			err = fmt.Errorf("%s login prompt characters did not match: %s", dbType, buf.String())
			return lcmd, err
		}
		buf.Write(bytes.TrimSpace(prompt[:nr]))
		if strings.Contains(buf.String(), prefix) {
			logger.Debugf("%s login prompt characters matched %s", dbType, buf.String())
			break
		}
	}
	close(done)
	return lcmd, nil
}

func DoLogin(opt *sqlOption, lcmd *localcommand.LocalCommand, dbType string) (*localcommand.LocalCommand, error) {
	//输入密码, 登录数据库
	time.Sleep(time.Millisecond * 100)
	_, err := lcmd.Write([]byte(opt.Password + "\r\n"))
	if err != nil {
		_ = lcmd.Close()
		logger.Errorf("%s local pty write err: %s", dbType, err)
		return lcmd, fmt.Errorf("%s conn err: %s", dbType, err)
	}
	//清除掉输入密码后，界面上显示的星号
	time.Sleep(time.Millisecond * 100)
	clearPassword := make([]byte, len(opt.Password)+2)
	_, _ = lcmd.Read(clearPassword)
	return lcmd, nil
}
