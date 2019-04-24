package service

import "cocogo/pkg/sdk"

func Authenticate(username, password, publicKey, remoteAddr, loginType string) *sdk.User {
	return &sdk.User{Id: "1111111111", Username: "admin", Name: "广宏伟"}
}

func GetUserProfile(userId string) (user sdk.User) {
	return
}

func LoadUserByUsername(user *sdk.User) {

}
