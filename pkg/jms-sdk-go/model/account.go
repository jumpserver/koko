package model

import "fmt"

type Account struct {
	Name       string `json:"name"`
	Username   string `json:"username"`
	Secret     string `json:"secret"`
	SecretType string `json:"secret_type"`
}

func (a *Account) String() string {
	return fmt.Sprintf("%s(%s)", a.Name, a.Username)
}
