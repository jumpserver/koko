package model

import (
	"fmt"
	"strings"
)

type BaseAsset struct {
	ID        string     `json:"id"`
	Address   string     `json:"address"`
	Name      string     `json:"name"`
	OrgID     string     `json:"org_id"`
	Protocols []Protocol `json:"protocols"`
}

type Asset struct {
	BaseAsset

	Domain   string `json:"domain"` // 是否需要走网域
	Comment  string `json:"comment"`
	OrgName  string `json:"org_name"`
	Platform string `json:"platform"`
	IsActive bool   `json:"is_active"` // 判断资产是否禁用
}

func (a *Asset) String() string {
	return fmt.Sprintf("%s(%s)", a.Name, a.Address)
}

func (a *Asset) ProtocolPort(protocol string) int {
	for _, item := range a.Protocols {
		protocolName := strings.ToLower(item.Name)
		if protocolName == strings.ToLower(protocol) {
			return item.Port
		}
	}
	return 0
}

func (a *Asset) IsSupportProtocol(protocol string) bool {
	for _, item := range a.Protocols {
		protocolName := strings.ToLower(item.Name)
		if protocolName == strings.ToLower(protocol) {
			return true
		}
	}
	return false
}

type Gateway struct {
	ID         string `json:"id"`
	Name       string `json:"Name"`
	IP         string `json:"ip"`
	Address    string `json:"address"`
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
