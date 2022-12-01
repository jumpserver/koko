package model

import "fmt"

type BaseAccount struct {
	Name       string `json:"name"`
	Username   string `json:"username"`
	Secret     string `json:"secret"`
	SecretType string `json:"secret_type"`
}

func (a *BaseAccount) String() string {
	return fmt.Sprintf("%s(%s)", a.Name, a.Username)
}

type Account struct {
	BaseAccount
	SuFrom *BaseAccount `json:"su_from"`
}

func (a *Account) GetBaseAccount() *BaseAccount {
	return &a.BaseAccount
}

type AccountDetail struct {
	Id         string     `json:"id"`
	Name       string     `json:"name"`
	Username   string     `json:"username"`
	Secret     string     `json:"secret"`
	SecretType LabelValue `json:"secret_type"`
	HasSecret  bool       `json:"has_secret"`
	IsActive   bool       `json:"is_active"`
	Privileged bool       `json:"privileged"`
}

type PermAccount struct {
	ID         string  `json:"id"`
	Name       string  `json:"name"`
	Username   string  `json:"username"`
	SecretType string  `json:"secret_type"`
	HasSecret  bool    `json:"has_secret"`
	Actions    Actions `json:"actions"`

	Secret string
}

func (a *PermAccount) String() string {
	return fmt.Sprintf("%s(%s)", a.Name, a.Username)
}
