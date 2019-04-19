package service

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/gliderlabs/ssh"

	"cocogo/pkg/common"
	"cocogo/pkg/config"
	"cocogo/pkg/logger"
	"cocogo/pkg/model"
)

var (
	AccessKeyNotFound     = errors.New("access key not found")
	AccessKeyFileNotFound = errors.New("access key file not found")
	AccessKeyInvalid      = errors.New("access key not valid")
)

type WrapperClient struct {
	common.Client
}

func (c *WrapperClient) ParseUrl(url string) string {
	if val, ok := urls[url]; ok {
		return val
	}
	return url
}

func NewClient() *WrapperClient {
	client := common.NewClient()
	return &WrapperClient{*client}
}

type Service struct {
	client    *WrapperClient
	Conf      *config.Config
	AccessKey AccessKey
}

func (s *Service) EnsureValidAuth() {
	for i := 0; i < 10; i++ {
		if !s.validateAuth() {
			msg := `Connect server error or access key is invalid,
			remove "./data/keys/.access_key" run again`
			logger.Error(msg)
			time.Sleep(time.Second * 3)

		} else {
			break
		}
		if i == 3 {
			os.Exit(1)
		}
	}
}

func (s *Service) SetAccessKey(key string) error {
	keySlice := strings.Split(strings.TrimSpace(s.Conf.AccessKey), ":")
	if len(key) != 2 {
		return AccessKeyInvalid
	}
	s.auth = AccessKey{
		Id:     keySlice[0],
		Secret: keySlice[1],
	}
	return nil
}

func (s *Service) LoadAccessKeyFromConfig() error {
	if s.Conf.AccessKey == "" {
		return AccessKeyNotFound
	}
	return s.SetAccessKey(s.Conf.AccessKey)
}

func (s *Service) LoadAccessKeyFromFile() error {
	if s.Conf.AccessKeyFile == "" {
		return AccessKeyNotFound
	}
	var accessKeyPath = ""
	if !path.IsAbs(s.Conf.AccessKeyFile) {
		accessKeyPath = filepath.Join(s.Conf.RootPath, s.Conf.AccessKeyFile)
	} else {
		accessKeyPath = s.Conf.AccessKeyFile
	}
	_, err := os.Stat(accessKeyPath)
	if err != nil {
		return AccessKeyFileNotFound
	}
	buf, err := ioutil.ReadFile(accessKeyPath)
	if err != nil {
		msg := fmt.Sprintf("read access key failed: %s", err)
		return errors.New(msg)
	}
	return s.SetAccessKey(string(buf))
}

func (s *Service) validateAuth() bool {

	url := fmt.Sprintf("%s%s", s.Conf.CoreHost, UserProfileUrl)
	body, err := s.SendHTTPRequest(http.MethodGet, url, nil)
	if err != nil {
		log.Info("Read response Body err:", err)
		return false
	}
	result := model.User{}
	err = json.Unmarshal(body, &result)
	if err != nil {
		log.Info("json.Unmarshal", err)
		return false
	}
	log.Info(result)
	return result != model.User{}
}

func (s *Service) CheckAuth(username, password, publicKey, remoteAddr, loginType string) (model.User, error) {
	/*
		{
		'token': '0191970b1f5b414bbae42ec8fbb2a2ad',
		'user':{'id': '34987591-bf75-4e5f-a102-6d59a1103431',
			'name': 'softwareuser1', 'username': 'softwareuser1',
			'email': 'xplz@hotmail.com',
			'groups': ['bdc861f9-f476-4554-9bd4-13c3112e469d'],
			'groups_display': '研发组', 'role': 'User',
			'role_display': '用户', 'avatar_url': '/static/img/avatar/user.png',
			'wechat': '', 'phone': None, 'otp_level': 0, 'comment': '',
			'source': 'local', 'source_display': 'Local', 'is_valid': True,
			'is_expired': False, 'is_active': True, 'created_by': 'admin',
			'is_first_login': True, 'date_password_last_updated': '2019-03-08 11:47:04 +0800',
			'date_expired': '2089-02-18 09:37:00 +0800'}}
	*/

	postMap := map[string]string{
		"username":    username,
		"password":    password,
		"public_key":  publicKey,
		"remote_addr": remoteAddr,
		"login_type":  loginType,
	}

	data, err := json.Marshal(postMap)
	if err != nil {
		log.Info(err)
		return model.User{}, err
	}

	url := fmt.Sprintf("%s%s", s.Conf.CoreHost, UserAuthUrl)
	body, err := s.SendHTTPRequest(http.MethodPost, url, data)

	if err != nil {
		log.Info("read body failed:", err)
		return model.User{}, err
	}
	var result struct {
		Token string     `json:"token"`
		User  model.User `json:"user"`
	}

	err = json.Unmarshal(body, &result)
	if err != nil {
		log.Info("json decode failed:", err)
		return model.User{}, err
	}

	return result.User, nil
}

func (s *Service) CheckSSHPassword(ctx ssh.Context, password string) bool {

	username := ctx.User()
	remoteAddr := ctx.RemoteAddr().String()
	authUser, err := s.CheckAuth(username, password, "", remoteAddr, "T")
	if err != nil {
		return false
	}
	ctx.SetValue("LoginUser", authUser)
	return true
}

func (s *Service) LoadTerminalConfig() {
	url := fmt.Sprintf("%s%s", s.Conf.CoreHost, TerminalConfigUrl)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		log.Info(err)
	}
	currentDate := HTTPGMTDate()
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Date", currentDate)
	req.Header.Set("Authorization", s.auth.Signature(currentDate))
	resp, err := s.http.Do(req)
	if err != nil {
		log.Info("client http request failed:", err)
	}

	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Info("Read response Body err:", err)
		return
	}
	fmt.Printf("%s\n", body)
	resBody := config.TerminalConfig{}
	err = json.Unmarshal(body, &resBody)
	if err != nil {
		log.Info("json.Unmarshal", err)
		return
	}
	s.Conf.TermConfig = &resBody
	fmt.Println(resBody)

}

func (s *Service) registerTerminalAndSave() error {

	postMap := map[string]string{
		"name":    s.Conf.Name,
		"comment": s.Conf.Comment,
	}
	data, err := json.Marshal(postMap)
	if err != nil {
		log.Info("json encode failed:", err)
		return err

	}
	url := fmt.Sprintf("%s%s", s.Conf.CoreHost, TerminalRegisterUrl)
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(data))
	if err != nil {
		log.Info("http NewRequest err:", err)
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", fmt.Sprintf("BootstrapToken %s", s.Conf.BootstrapToken))
	resp, err := s.http.Do(req)
	if err != nil {
		log.Info("http request err:", err)
		return err

	}
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Info("read resp body err:", err)
		return err
	}
	/*
		{
				    "name": "sss2",
				    "comment": "Coco",
				    "service_account": {
				        "id": "c2dece80-1811-42bc-bd5b-aef0f4180263",
				        "name": "sss2",
				        "access_key": {
				            "id": "f9b2cf91-7f30-45ea-9edf-b73ec0f48d5a",
				            "secret": "fd083b6c-e823-47bf-870c-0dd6051e69f1"
				        }
				    }
				}
	*/
	log.Infof("%s", body)

	var resBody struct {
		ServiceAccount struct {
			Id        string `json:"id"`
			Name      string `json:"name"`
			Accesskey struct {
				Id     string `json:"id"`
				Secret string `json:"secret"`
			} `json:"access_key"`
		} `json:"service_account"`
	}

	err = json.Unmarshal(body, &resBody)
	if err != nil {
		log.Info("json Unmarshal:", err)
		return err
	}
	if resBody.ServiceAccount.Name == "" {
		return errors.New(string(body))
	}

	s.auth = accessAuth{
		accessKey:    resBody.ServiceAccount.Accesskey.Id,
		accessSecret: resBody.ServiceAccount.Accesskey.Secret,
	}
	return s.saveAccessKey()
}

func (s *Service) saveAccessKey() error {
	MakeSureDirExit(s.Conf.AccessKeyFile)
	f, err := os.Create(s.Conf.AccessKeyFile)
	fmt.Println("Create file path:", s.Conf.AccessKeyFile)
	if err != nil {
		return err
	}
	keyAndSecret := fmt.Sprintf("%s:%s", s.auth.accessKey, s.auth.accessSecret)
	_, err = f.WriteString(keyAndSecret)
	if err != nil {
		return err
	}
	err = f.Close()
	if err != nil {
		return err
	}
	return nil
}
