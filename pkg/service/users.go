package service

import (
	"context"
	"fmt"
	"time"

	"github.com/pkg/errors"

	"github.com/jumpserver/koko/pkg/common"
	"github.com/jumpserver/koko/pkg/logger"
	"github.com/jumpserver/koko/pkg/model"
)

type authResponse struct {
	Err  string       `json:"error,omitempty"`
	Msg  string       `json:"msg,omitempty"`
	Data dataResponse `json:"data,omitempty"`

	Username    string `json:"username,omitempty"`
	Token       string `json:"token,omitempty"`
	Keyword     string `json:"keyword,omitempty"`
	DateExpired string `json:"date_expired,omitempty"`

	User model.User `json:"user,omitempty"`
}

type dataResponse struct {
	Choices []string `json:"choices,omitempty"`
	Url     string   `json:"url,omitempty"`
}

type AuthOptions struct {
	Name string
	Url  string
}

func NewSessionClient(setters ...SessionOption) SessionClient {
	option := &SessionOptions{}
	for _, setter := range setters {
		setter(option)
	}
	conn := newClient()
	conn.SetHeader("X-Forwarded-For", option.RemoteAddr)
	conn.SetHeader("X-JMS-LOGIN-TYPE", option.LoginType)
	return SessionClient{
		option:      option,
		client:      &conn,
		authOptions: make(map[string]AuthOptions),
	}
}

type SessionClient struct {
	option *SessionOptions
	client *common.Client

	authOptions map[string]AuthOptions
}

func (u *SessionClient) SetOption(setters ...SessionOption) {
	for _, setter := range setters {
		setter(u.option)
	}
}

func (u *SessionClient) Authenticate(ctx context.Context) (user model.User, authStatus AuthStatus) {
	authStatus = AuthFailed
	data := map[string]string{
		"username":    u.option.Username,
		"password":    u.option.Password,
		"public_key":  u.option.PublicKey,
		"remote_addr": u.option.RemoteAddr,
		"login_type":  u.option.LoginType,
	}
	var resp authResponse
	_, err := u.client.Post(UserTokenAuthURL, data, &resp)
	if err != nil {
		logger.Errorf("User %s Authenticate err: %s", u.option.Username, err)
		return
	}
	if resp.Err != "" {
		switch resp.Err {
		case ErrLoginConfirmWait:
			logger.Infof("User %s login need confirmation", u.option.Username)
			authStatus = AuthConfirmRequired
		case ErrMFARequired:
			for _, item := range resp.Data.Choices {
				u.authOptions[item] = AuthOptions{
					Name: item,
					Url:  resp.Data.Url,
				}
			}
			logger.Infof("User %s login need MFA", u.option.Username)
			authStatus = AuthMFARequired
		default:
			logger.Errorf("User %s login err: %s", u.option.Username, resp.Err)
		}
		return
	}
	if resp.Token != "" {
		return resp.User, AuthSuccess
	}
	return
}

func (u *SessionClient) CheckUserOTP(ctx context.Context, code string) (user model.User, authStatus AuthStatus) {
	var err error
	authStatus = AuthFailed
	data := map[string]string{
		"code":        code,
		"remote_addr": u.option.RemoteAddr,
		"login_type":  u.option.LoginType,
	}
	for name, authData := range u.authOptions {
		var resp authResponse
		switch name {
		case "opt":
			data["type"] = name
		}
		_, err = u.client.Post(authData.Url, data, &resp)
		if err != nil {
			logger.Errorf("User %s use %s check MFA err: %s", u.option.Username, name, err)
			continue
		}
		if resp.Err != "" {
			logger.Errorf("User %s use %s check MFA err: %s", u.option.Username, name, resp.Err)
			continue
		}
		if resp.Msg == "ok" {
			logger.Infof("User %s check MFA success, check if need admin confirm", u.option.Username)
			return u.Authenticate(ctx)
		}
	}
	logger.Errorf("User %s failed to check MFA", u.option.Username)
	return
}

func (u *SessionClient) CheckConfirm(ctx context.Context) (user model.User, authStatus AuthStatus) {
	var err error
	t := time.NewTicker(5 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			logger.Errorf("User %s exit and cancel confirmation", u.option.Username)
			u.CancelConfirm()
			return
		case <-t.C:
			var resp authResponse
			_, err = u.client.Get(UserConfirmAuthURL, &resp)
			if err != nil {
				logger.Errorf("User %s check confirm err: %s", u.option.Username, err)
				return
			}
			if resp.Err != "" {
				switch resp.Err {
				case ErrLoginConfirmWait:
					logger.Infof("User %s still wait confirm", u.option.Username)
					continue
				case ErrLoginConfirmRejected:
					logger.Infof("User %s confirmation was rejected by admin", u.option.Username)
				default:
					logger.Infof("User %s confirmation was rejected by err: %s", u.option.Username, resp.Err)
				}
				return
			}
			if resp.Msg == "ok" {
				logger.Infof("User %s confirmation was accepted", u.option.Username)
				return u.Authenticate(ctx)
			}
		}
	}
}

func (u *SessionClient) CancelConfirm() {
	_, err := u.client.Delete(UserConfirmAuthURL, nil)
	if err != nil {
		logger.Errorf("Cancel User %s confirmation err: %s", u.option.Username, err)
		return
	}
	logger.Infof("Cancel User %s confirmation success", u.option.Username)
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

func CheckUserCookie(sessionID, csrfToken string) (user *model.User, err error) {
	cli := newClient()
	cli.SetCookie("csrftoken", csrfToken)
	cli.SetCookie("sessionid", sessionID)
	_, err = cli.Get(UserProfileURL, &user)
	return
}
