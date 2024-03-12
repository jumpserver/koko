package model

import (
	"crypto/md5"
	"fmt"
)

type BaseAccount struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	Username   string     `json:"username"`
	Secret     string     `json:"secret"`
	SecretType LabelValue `json:"secret_type"`
}

func (a *BaseAccount) String() string {
	return fmt.Sprintf("%s(%s)", a.Name, a.Username)
}

func (a *BaseAccount) HashId() string {
	content := fmt.Sprintf("%s_%s", a.Username, a.Secret)
	return fmt.Sprintf("%x", md5.Sum([]byte(content)))
}

func (a *BaseAccount) IsSSHKey() bool {
	return a.SecretType.Value == "ssh_key"
}

// 如果是 null，表示这个账号是一个空用户名

func (a *BaseAccount) IsNull() bool {
	return a.Username == "null"
}
func (a *BaseAccount) IsAnonymous() bool {
	return a.Username == ANONUser
}

type Account struct {
	BaseAccount
	SuFrom *BaseAccount `json:"su_from"`
}

func (a *Account) GetBaseAccount() *BaseAccount {
	return &a.BaseAccount
}

type AccountDetail struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	Username   string     `json:"username"`
	Secret     string     `json:"secret"`
	SecretType LabelValue `json:"secret_type"`
	HasSecret  bool       `json:"has_secret"`
	IsActive   bool       `json:"is_active"`
	Privileged bool       `json:"privileged"`
}

type PermAccount struct {
	Name       string  `json:"name"`
	Username   string  `json:"username"`
	SecretType string  `json:"secret_type"`
	HasSecret  bool    `json:"has_secret"`
	Actions    Actions `json:"actions"`
	Alias      string  `json:"alias"`

	Secret string
}

func (a *PermAccount) IsSSHKey() bool {
	return a.SecretType == "ssh_key"
}

func (a *PermAccount) String() string {
	return fmt.Sprintf("%s(%s)", a.Name, a.Username)
}

func (a *PermAccount) IsAnonymous() bool {
	return a.Username == ANONUser
}

func (a *PermAccount) IsInputUser() bool {
	return a.Username == InputUser
}

const (
	InputUser   = "@INPUT"
	DynamicUser = "@USER"
	ANONUser    = "@ANON"
)

type PermAccountList []PermAccount

func (a PermAccountList) Len() int {
	return len(a)
}

func (a PermAccountList) Swap(i, j int) {
	a[i], a[j] = a[j], a[i]
}

func (a PermAccountList) Less(i, j int) bool {
	return a[i].Name < a[j].Name
}
