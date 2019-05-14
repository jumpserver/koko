package proxy

import (
	"fmt"
	"testing"
)

var testConnection = ServerSSHConnection{
	host:     "127.0.0.1",
	port:     "22",
	user:     "root",
	password: "redhat",
	Proxy:    &ServerSSHConnection{host: "192.168.244.185", port: "22", user: "root", password: "redhat"},
}

func TestSSHConnection_Config(t *testing.T) {
	config, err := testConnection.Config()
	if err != nil {
		t.Errorf("Get config error %s", err)
	}
	fmt.Println(config.User)
}

func TestSSHConnection_Connect(t *testing.T) {
	err := testConnection.Connect(24, 80, "xterm")
	if err != nil {
		t.Errorf("Connect error %s", err)
	}
}
