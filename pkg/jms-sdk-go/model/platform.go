package model

import "strings"

type Platform struct {
	BaseOs   string                 `json:"base"`
	MetaData map[string]interface{} `json:"meta"`

	ID   int    `json:"id"`
	Name string `json:"name"`

	Protocols     PlatformProtocols `json:"protocols"`
	Category      LabelValue        `json:"category"`
	Charset       LabelValue        `json:"charset"`
	Type          LabelValue        `json:"type"`
	SuEnabled     bool              `json:"su_enabled"`
	SuMethod      string            `json:"su_method"`
	DomainEnabled bool              `json:"domain_enabled"`
	Comment       string            `json:"comment"`
}

type PlatformProtocols []PlatformProtocol

func (p PlatformProtocols) GetSftpPath(protocol string) string {
	for i := range p {
		if strings.ToLower(p[i].Name) == strings.ToLower(protocol) {
			return p[i].SftpHome
		}
	}
	return "/tmp"
}

type PlatformProtocol struct {
	Protocol
	ProtocolSetting
}

type ProtocolSetting struct {
	Security         string `json:"security"`
	SftpEnabled      bool   `json:"sftp_enabled"`
	SftpHome         string `json:"sftp_home"`
	AutoFill         bool   `json:"auto_fill"`
	UsernameSelector string `json:"username_selector"`
	PasswordSelector string `json:"password_selector"`
	SubmitSelector   string `json:"submit_selector"`
}

type Protocol struct {
	Id   int    `json:"id"`
	Name string `json:"name"`
	Port int    `json:"port"`
}

type LabelValue struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

type Action LabelValue

type SecretType LabelValue
