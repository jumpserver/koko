package service

import (
	"fmt"

	"cocogo/pkg/logger"
	"cocogo/pkg/model"
)

func Authenticate(username, password, publicKey, remoteAddr, loginType string) (resp *model.AuthResponse, err error) {
	data := map[string]string{
		"username":    username,
		"password":    password,
		"public_key":  publicKey,
		"remote_addr": remoteAddr,
		"login_type":  loginType,
	}
	Url := client.ParseUrlQuery(UserAuthURL, nil)
	err = client.Post(Url, data, resp)
	if err != nil {
		logger.Error(err)
	}
	return
}

func AuthenticateMFA(seed, code, loginType string) (resp *model.AuthResponse, err error) {
	/*
		data = {
		            'seed': seed,
		            'otp_code': otp_code,
		            'login_type': login_type,
		        }

	*/

	data := map[string]string{
		"seed":       seed,
		"otp_code":   code,
		"login_type": loginType,
	}

	Url := client.ParseUrlQuery(AuthMFAURL, nil)
	err = client.Post(Url, data, resp)
	if err != nil {
		logger.Error(err)
	}
	return

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
