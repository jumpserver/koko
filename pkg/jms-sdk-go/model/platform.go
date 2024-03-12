package model

import "strings"

type Platform struct {
	BaseOs   string                 `json:"base"`
	MetaData map[string]interface{} `json:"meta"`

	ID   int    `json:"id"`
	Name string `json:"name"`

	Protocols PlatformProtocols `json:"protocols"`
	Category  LabelValue        `json:"category"`
	Charset   LabelValue        `json:"charset"`
	Type      LabelValue        `json:"type"`
	SuEnabled bool              `json:"su_enabled"`
	SuMethod  *LabelValue       `json:"su_method,omitempty"`
	//DomainEnabled bool              `json:"domain_enabled"`
	Comment string `json:"comment"`
}

type PlatformProtocols []PlatformProtocol

func (p PlatformProtocols) GetSftpPath(protocol string) string {
	for i := range p {
		if strings.EqualFold(p[i].Name, protocol) {
			return p[i].Setting.SftpHome
		}
	}
	return "/tmp"
}

func (p Platform) GetProtocol(protocol string) PlatformProtocol {
	for i := range p.Protocols {
		item := p.Protocols[i]
		if strings.EqualFold(item.Name, protocol) {
			return item
		}
	}
	return PlatformProtocol{}
}

type PlatformProtocol struct {
	Protocol
	Setting ProtocolSetting `json:"setting"`
}

type ProtocolSetting struct {
	Security         string `json:"security"`
	SftpEnabled      bool   `json:"sftp_enabled"`
	SftpHome         string `json:"sftp_home"`
	AutoFill         bool   `json:"auto_fill"`
	UsernameSelector string `json:"username_selector"`
	PasswordSelector string `json:"password_selector"`
	SubmitSelector   string `json:"submit_selector"`

	// for redis 特殊处理的字段
	AuthUsername bool `json:"auth_username"`

	TelnetUsernamePrompt string `json:"username_prompt"`
	TelnetPasswordPrompt string `json:"password_prompt"`
	TelnetSuccessPrompt  string `json:"success_prompt"`

	// for mongodb
	AuthSource string `json:"auth_source"`
}

type Protocol struct {
	Id     int    `json:"id"`
	Name   string `json:"name"`
	Port   int    `json:"port"`
	Public bool   `json:"public"`
}

type LabelValue struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

type Action LabelValue

type SecretType LabelValue
