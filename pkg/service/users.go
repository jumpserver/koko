package service

import (
	"fmt"

	"cocogo/pkg/logger"
	"cocogo/pkg/model"
)

func Authenticate(username, password, publicKey, remoteAddr, loginType string) (user *model.User) {
	data := map[string]string{
		"username":    username,
		"password":    password,
		"public_key":  publicKey,
		"remote_addr": remoteAddr,
		"login_type":  loginType,
	}
	var resp struct {
		Token string      `json:"token"`
		User  *model.User `json:"user"`
	}
	Url := client.ParseUrlQuery(UserAuthURL, nil)
	err := client.Post(Url, data, &resp)
	if err != nil {
		logger.Error(err)
	}
	return resp.User
}

func GetUserProfile(userId string) (user *model.User) {
	Url := authClient.ParseUrlQuery(fmt.Sprintf(UserUserURL, userId), nil)
	err := authClient.Get(Url, &user)
	if err != nil {
		logger.Error(err)
	}
	return
}

func CheckUserCookie(sessionId, csrfToken string) (user *model.User) {
	client.SetCookie("csrftoken", csrfToken)
	client.SetCookie("sessionid", sessionId)
	Url := client.ParseUrlQuery(UserProfileURL, nil)
	err := client.Get(Url, &user)
	if err != nil {
		logger.Error(err)
	}
	return
}
