package srvconn

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
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
	ProtocolMySQL  = "mysql"

	ProtocolMariadb = "mariadb"
)

var (
	ErrUnSupportedProtocol = errors.New("unsupported protocol")

	ErrKubectlClient = errors.New("not found Kubectl client")

	ErrMySQLClient = errors.New("not found MySQL client")
)

type supportedChecker func() error

var supportedMap = map[string]supportedChecker{
	ProtocolSSH:     builtinSupported,
	ProtocolTELNET:  builtinSupported,
	ProtocolK8s:     kubectlSupported,
	ProtocolMySQL:   mySQLSupported,
	ProtocolMariadb: mySQLSupported,
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
