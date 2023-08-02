package model

import (
	"fmt"
	"strings"
)

type SecretInfo struct {
	CaCert     string `json:"ca_cert"`
	ClientCert string `json:"client_cert"`
	ClientKey  string `json:"client_key"`
}

type SpecInfo struct {
	// database
	DBName string `json:"db_name"`

	UseSSL           bool `json:"use_ssl"`
	AllowInvalidCert bool `json:"allow_invalid_cert"`

	// web
	Autofill         string `json:"autofill"`
	UsernameSelector string `json:"username_selector"`
	PasswordSelector string `json:"password_selector"`
	SubmitSelector   string `json:"submit_selector"`
}

type Asset struct {
	ID         string       `json:"id"`
	Address    string       `json:"address"`
	Name       string       `json:"name"`
	OrgID      string       `json:"org_id"`
	Protocols  []Protocol   `json:"protocols"`
	SpecInfo   SpecInfo     `json:"spec_info"`
	SecretInfo SecretInfo   `json:"secret_info"`
	Platform   BasePlatform `json:"platform"`

	Domain *BaseDomain `json:"domain"` // token 方式获取的资产，domain 为 nil

	Comment  string `json:"comment"`
	OrgName  string `json:"org_name"`
	IsActive bool   `json:"is_active"` // 判断资产是否禁用

	Accounts Actions `json:"accounts,omitempty"` // 只有 detail api才会有这个字段
}

type BaseDomain struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type BasePlatform struct {
	Id   int    `json:"id"`
	Name string `json:"name"`
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

func (a *Asset) SupportProtocols() []string {
	protocols := make([]string, 0, len(a.Protocols))
	for _, item := range a.Protocols {
		if item.Public {
			protocols = append(protocols, item.Name)
		}
	}
	return protocols
}

func (a *Asset) FilterProtocols(filter func(string) bool) []string {
	protocols := make([]string, 0, len(a.Protocols))
	for _, item := range a.Protocols {
		if item.Public {
			if filter != nil && !filter(item.Name) {
				continue
			}
			protocols = append(protocols, item.Name)
		}
	}
	return protocols
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
	ID        string    `json:"id"`
	Name      string    `json:"Name"`
	Address   string    `json:"address"`
	Protocols Protocols `json:"protocols"`
	Account   Account   `json:"account"`
}

type Protocols []Protocol

func (p Protocols) GetProtocolPort(protocol string) int {
	for i := range p {
		if strings.EqualFold(p[i].Name, protocol) {
			return p[i].Port
		}
	}
	return 0
}
func (p Protocols) IsSupportProtocol(protocol string) bool {
	for _, item := range p {
		protocolName := strings.ToLower(item.Name)
		if protocolName == strings.ToLower(protocol) {
			return true
		}
	}
	return false
}

type Domain struct {
	ID       string    `json:"id"`
	Name     string    `json:"name"`
	Gateways []Gateway `json:"gateways"`
}

const (
	ProtocolSSH    = "ssh"
	ProtocolTelnet = "telnet"
	ProtocolK8S    = "k8s"
	ProtocolSFTP   = "sftp"
	ProtocolRedis  = "redis"
)
