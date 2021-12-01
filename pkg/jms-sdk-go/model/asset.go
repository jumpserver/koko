package model

import (
	"fmt"
	"strconv"
	"strings"
)

type Asset struct {
	ID        string   `json:"id"`
	Hostname  string   `json:"hostname"`
	IP        string   `json:"ip"`
	Os        string   `json:"os"`
	Domain    string   `json:"domain"` // 是否需要走网域
	Comment   string   `json:"comment"`
	Protocols []string `json:"protocols"`
	OrgID     string   `json:"org_id"`
	OrgName   string   `json:"org_name"`
	Platform  string   `json:"platform"`
	IsActive  bool     `json:"is_active"` // 判断资产是否禁用
}

func (a *Asset) String() string {
	return fmt.Sprintf("%s(%s)", a.Hostname, a.IP)
}

func (a *Asset) ProtocolPort(protocol string) int {
	for _, item := range a.Protocols {
		if strings.Contains(strings.ToLower(item), strings.ToLower(protocol)) {
			proAndPort := strings.Split(item, "/")
			if len(proAndPort) == 2 {
				if port, err := strconv.Atoi(proAndPort[1]); err == nil {
					return port
				}
			}
		}
	}
	return 0
}

func (a *Asset) IsSupportProtocol(protocol string) bool {
	for _, item := range a.Protocols {
		if strings.Contains(strings.ToLower(item), strings.ToLower(protocol)) {
			return true
		}
	}
	return false
}

type Gateway struct {
	ID         string `json:"id"`
	Name       string `json:"Name"`
	IP         string `json:"ip"`
	Port       int    `json:"port"`
	Protocol   string `json:"protocol"`
	Username   string `json:"username"`
	Password   string `json:"password"`
	PrivateKey string `json:"private_key"`
}

type Domain struct {
	ID       string    `json:"id"`
	Gateways []Gateway `json:"gateways"`
	Name     string    `json:"name"`
}

const (
	ProtocolSSH    = "ssh"
	ProtocolTelnet = "telnet"
	ProtocolK8S    = "k8s"
	ProtocolMysql  = "mysql"
)
