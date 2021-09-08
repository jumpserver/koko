package service

import (
	"github.com/jumpserver/koko/pkg/jms-sdk-go/httplib"
	"github.com/jumpserver/koko/pkg/jms-sdk-go/model"
)

func NewUserClient(setters ...UserClientOption) *UserClient {
	opts := &UserClientOptions{}
	for _, setter := range setters {
		setter(opts)
	}
	if opts.RemoteAddr != "" {
		opts.client.SetHeader("X-Forwarded-For", opts.RemoteAddr)
	}
	if opts.LoginType != "" {
		opts.client.SetHeader("X-JMS-LOGIN-TYPE", opts.LoginType)
	}
	return &UserClient{
		client: opts.client,
		Opts:   opts,
	}
}

type UserClient struct {
	client *httplib.Client
	Opts   *UserClientOptions
}

func (u *UserClient) SetOption(setters ...UserClientOption) {
	for _, setter := range setters {
		setter(u.Opts)
	}
}

func (u *UserClient) GetAPIToken() (resp AuthResponse, err error) {
	data := map[string]string{
		"username":    u.Opts.Username,
		"password":    u.Opts.Password,
		"public_key":  u.Opts.PublicKey,
		"remote_addr": u.Opts.RemoteAddr,
		"login_type":  u.Opts.LoginType,
	}
	_, err = u.client.Post(UserTokenAuthURL, data, &resp)
	return
}

func (u *UserClient) CheckConfirmAuthStatus() (resp AuthResponse, err error) {
	_, err = u.client.Get(UserConfirmAuthURL, &resp)
	return
}

func (u *UserClient) CancelConfirmAuth() (err error) {
	_, err = u.client.Delete(UserConfirmAuthURL, nil)
	return
}

func (u *UserClient) SendOTPRequest(optReq *OTPRequest) (resp AuthResponse, err error) {
	_, err = u.client.Post(optReq.ReqURL, optReq.ReqBody, &resp)
	return
}

func (u *UserClient) SelectMFAChoice(mfaType string) (err error) {
	data := map[string]string{
		"type": mfaType,
	}
	_, err = u.client.Post(AuthMFASelectURL, data, nil)
	return
}

type OTPRequest struct {
	ReqURL  string
	ReqBody map[string]interface{}
}

type DataResponse struct {
	Choices []string `json:"choices,omitempty"`
	Url     string   `json:"url,omitempty"`
}

type AuthResponse struct {
	Err  string       `json:"error,omitempty"`
	Msg  string       `json:"msg,omitempty"`
	Data DataResponse `json:"data,omitempty"`

	Username    string `json:"username,omitempty"`
	Token       string `json:"token,omitempty"`
	Keyword     string `json:"keyword,omitempty"`
	DateExpired string `json:"date_expired,omitempty"`

	User model.User `json:"user,omitempty"`
}

type UserClientOption func(*UserClientOptions)

func UserClientUsername(username string) UserClientOption {
	return func(args *UserClientOptions) {
		args.Username = username
	}
}

func UserClientPassword(password string) UserClientOption {
	return func(args *UserClientOptions) {
		args.Password = password
	}
}

func UserClientPublicKey(publicKey string) UserClientOption {
	return func(args *UserClientOptions) {
		args.PublicKey = publicKey
	}
}

func UserClientRemoteAddr(remoteAddr string) UserClientOption {
	return func(args *UserClientOptions) {
		args.RemoteAddr = remoteAddr
	}
}

func UserClientLoginType(loginType string) UserClientOption {
	return func(args *UserClientOptions) {
		args.LoginType = loginType
	}
}

func UserClientHttpClient(con *httplib.Client) UserClientOption {
	return func(args *UserClientOptions) {
		args.client = con
	}
}

type UserClientOptions struct {
	Username   string
	Password   string
	PublicKey  string
	RemoteAddr string
	LoginType  string
	client     *httplib.Client
}
