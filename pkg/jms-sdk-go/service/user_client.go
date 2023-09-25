package service

import (
	"bytes"
	"crypto/aes"
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
	"time"

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

const (
	svcHeader = "X-JMS-SVC"
)

func (u *UserClient) GetAPIToken() (resp AuthResponse, err error) {
	data := map[string]string{
		"username":    u.Opts.Username,
		"password":    u.Opts.Password,
		"public_key":  u.Opts.PublicKey,
		"remote_addr": u.Opts.RemoteAddr,
		"login_type":  u.Opts.LoginType,
	}
	ak := u.Opts.signKey
	// 移除 Secret 中的 "-", 保证长度为 32
	secretKey := strings.ReplaceAll(ak.Secret, "-", "")
	encryptKey, err := GenerateEncryptKey(secretKey)
	if err != nil {
		return resp, err
	}
	signKey := fmt.Sprintf("%s:%s", ak.ID, encryptKey)
	u.client.SetHeader(svcHeader, fmt.Sprintf("Sign %s", signKey))
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

func UserClientSvcSignKey(key model.AccessKey) UserClientOption {
	return func(args *UserClientOptions) {
		args.signKey = key
	}
}

func GenerateEncryptKey(key string) (string, error) {
	seconds := time.Now().Unix()
	value := strconv.FormatUint(uint64(seconds), 10)
	return EncryptECB(value, key)
}

type UserClientOptions struct {
	Username   string
	Password   string
	PublicKey  string
	RemoteAddr string
	LoginType  string
	client     *httplib.Client

	signKey model.AccessKey
}

func EncryptECB(plaintext string, key string) (string, error) {
	block, err := aes.NewCipher([]byte(key))
	if err != nil {
		return "", err
	}
	newPlaintext := make([]byte, 0, len(plaintext))
	newPlaintext = append(newPlaintext, []byte(plaintext)...)
	if len(newPlaintext)%aes.BlockSize != 0 {
		padding := aes.BlockSize - len(plaintext)%aes.BlockSize
		newPlaintext = append(newPlaintext, bytes.Repeat([]byte{byte(0x00)}, padding)...)
	}

	ciphertext := make([]byte, len(newPlaintext))
	for i := 0; i < len(newPlaintext); i += aes.BlockSize {
		block.Encrypt(ciphertext[i:i+aes.BlockSize], newPlaintext[i:i+aes.BlockSize])
	}
	ret := base64.StdEncoding.EncodeToString(ciphertext)
	return ret, nil
}

func DecryptECB(ciphertext string, key string) (string, error) {
	ret, err := base64.StdEncoding.DecodeString(ciphertext)
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher([]byte(key))
	if err != nil {
		return "", err
	}

	if len(ret)%aes.BlockSize != 0 {
		return "", fmt.Errorf("ciphertext is not a multiple of the block size")
	}
	plaintext := make([]byte, len(ret))
	for i := 0; i < len(ret); i += aes.BlockSize {
		block.Decrypt(plaintext[i:i+aes.BlockSize], ret[i:i+aes.BlockSize])
	}

	// 移除 Zero 填充
	for len(plaintext) > 0 && plaintext[len(plaintext)-1] == 0x00 {
		plaintext = plaintext[:len(plaintext)-1]
	}
	return string(plaintext), nil
}
