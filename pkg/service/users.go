package service

import (
	"cocogo/pkg/model"
)

func Authenticate(username, password, publicKey, remoteAddr, loginType string) *model.User {
	return &model.User{Id: "1111111111", Username: "admin", Name: "广宏伟"}
}

func GetUserProfile(userId string) (user model.User) {
	return
}

func LoadUserByUsername(user *model.User) {

}
