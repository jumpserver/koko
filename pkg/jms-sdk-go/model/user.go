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

type UserKokoPreference struct {
	Basic KokoBasic `json:"basic"`
}
type KokoBasic struct {
	ThemeName string `json:"terminal_theme_name"`
}
