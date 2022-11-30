package model

import "fmt"

type Account struct {
	Name       string `json:"name"`
	Username   string `json:"username"`
	Secret     string `json:"secret"`
	SecretType string `json:"secret_type"`
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

func (a *Account) String() string {
	return fmt.Sprintf("%s(%s)", a.Name, a.Username)
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
