package service

import (
	"fmt"

	"github.com/pkg/errors"

	"cocogo/pkg/logger"
	"cocogo/pkg/model"
)

type AuthResp struct {
	Token string      `json:"token"`
	Seed  string      `json:"seed"`
	User  *model.User `json:"user"`
}

func Authenticate(username, password, publicKey, remoteAddr, loginType string) (resp *AuthResp, err error) {
	data := map[string]string{
		"username":    username,
		"password":    password,
		"public_key":  publicKey,
		"remote_addr": remoteAddr,
		"login_type":  loginType,
	}
	Url := client.ParseUrl(UserAuthURL, nil)
	err = client.Post(Url, data, &resp)

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

	Url := client.ParseUrl(AuthMFAURL, nil)
	err = client.Post(Url, data, resp)
	if err != nil {
		logger.Error(err)
	}
	return

}

func GetUserProfile(userId string) (user *model.User) {
	Url := fmt.Sprintf(UserDetailURL, userId)
	err := authClient.Get(Url, user)
	if err != nil {
		logger.Error(err)
	}
	return
}

func GetProfile() (user *model.User, err error) {
	err = authClient.Get(UserProfileURL, &user)
	return user, err
}

func GetUserByUsername(username string) (user *model.User, err error) {
	var users []*model.User
	payload := map[string]string{"username": username}
	err = authClient.Get(UserListUrl, &users, payload)
	if err != nil {
		return
	}
	if len(users) != 1 {
		err = errors.New(fmt.Sprintf("Not found user by username: %s", username))
	} else {
		user = users[0]
	}
	return
}

func CheckUserOTP(seed, code string) (resp *AuthResp, err error) {
	data := map[string]string{
		"seed":     seed,
		"otp_code": code,
	}
	err = client.Post(UserAuthOTPURL, data, &resp)
	if err != nil {
		return
	}
	return
}

func CheckUserCookie(sessionId, csrfToken string) (user *model.User) {
	client.SetCookie("csrftoken", csrfToken)
	client.SetCookie("sessionid", sessionId)
	err := client.Get(UserProfileURL, &user)
	if err != nil {
		logger.Error(err)
	}
	return
}
