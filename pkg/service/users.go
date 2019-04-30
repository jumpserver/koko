package service

import (
	"fmt"

	"cocogo/pkg/logger"
	"cocogo/pkg/model"
)

func Authenticate(username, password, publicKey, remoteAddr, loginType string) (user model.User) {
	data := map[string]string{
		"username":    username,
		"password":    password,
		"public_key":  publicKey,
		"remote_addr": remoteAddr,
		"login_type":  loginType}
	var resp struct {
		Token string     `json:"token"`
		User  model.User `json:"user"`
	}

	err := client.Post(baseHost+UserAuthURL, data, &resp)
	if err != nil {
		logger.Error(err)
	}
	return resp.User
}

func GetUserProfile(userId string) (user model.User) {
	Url := fmt.Sprintf(baseHost+UserUserURL, userId)
	err := authClient.Get(Url, &user)
	if err != nil {
		logger.Error(err)
	}
	return
}

func CheckUserCookie(sessionId, csrfToken string) (user model.User) {
	client.SetCookie("csrftoken", csrfToken)
	client.SetCookie("sessionid", sessionId)
	err := client.Get(baseHost+UserProfileURL, &user)
	if err != nil {
		logger.Error(err)
	}
	return
}
