package model

import (
	"fmt"
)

type User struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Username string `json:"username"`
	Email    string `json:"email"`
	Role     string `json:"role"`
	IsValid  bool   `json:"is_valid"`
	IsActive bool   `json:"is_active"`
	OTPLevel int    `json:"otp_level"`
}

type MiniUser struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Username string `json:"username"`
}

func (u *User) String() string {
	return fmt.Sprintf("%s(%s)", u.Name, u.Username)
}

type TokenUser struct {
	UserID         string `json:"user"`
	UserName       string `json:"username"`
	AssetID        string `json:"asset"`
	Hostname       string `json:"hostname"`
	SystemUserID   string `json:"system_user"`
	SystemUserName string `json:"system_user_name"`
}
