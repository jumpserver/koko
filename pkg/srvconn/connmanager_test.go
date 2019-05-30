package srvconn

import (
	"fmt"
	"testing"
)

var testConnection = SSHClientConfig{
	Host:     "127.0.0.1",
	Port:     "22",
	User:     "root",
	Password: "redhat",
	Proxy:    &SSHClientConfig{Host: "192.168.244.185", Port: "22", User: "root", Password: "redhat"},
}

func TestSSHConnection_Config(t *testing.T) {
	config, err := testConnection.Config()
	if err != nil {
		t.Errorf("Get config error %s", err)
	}
	fmt.Println(config.User)
}

func TestSSHConnection_Connect(t *testing.T) {
	_, err := testConnection.Dial()
	if err != nil {
		t.Errorf("Connect error %s", err)
	}
}
