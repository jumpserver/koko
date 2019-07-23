package service

import (
	"fmt"

	"github.com/pkg/errors"

	"github.com/jumpserver/koko/pkg/logger"
	"github.com/jumpserver/koko/pkg/model"
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
	_, err = client.Post(UserAuthURL, data, &resp)
	return
}

func GetUserDetail(userID string) (user *model.User) {
	Url := fmt.Sprintf(UserDetailURL, userID)
	_, err := authClient.Get(Url, &user)
	if err != nil {
		logger.Error(err)
	}
	return
}

func GetProfile() (user *model.User, err error) {
	_, err = authClient.Get(UserProfileURL, &user)
	return user, err
}

func GetUserByUsername(username string) (user *model.User, err error) {
	var users []*model.User
	payload := map[string]string{"username": username}
	_, err = authClient.Get(UserListURL, &users, payload)
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
	_, err = client.Post(UserAuthOTPURL, data, &resp)
	if err != nil {
		return
	}
	return
}

func CheckUserCookie(sessionID, csrfToken string) (user *model.User, err error) {
	cli := newClient()
	cli.SetCookie("csrftoken", csrfToken)
	cli.SetCookie("sessionid", sessionID)
	_, err = cli.Get(UserProfileURL, &user)
	return
}
