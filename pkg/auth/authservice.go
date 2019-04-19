package auth

import (
	"bytes"
	"cocogo/pkg/config"
	"cocogo/pkg/model"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/gliderlabs/ssh"

	"github.com/sirupsen/logrus"
)

var log = logrus.New()

func NewAuthService(conf *config.Config) *Service {
	return &Service{
		http: &http.Client{},
		Conf: conf,
	}
}

type Service struct {
	http *http.Client
	Conf *config.Config
	auth accessAuth
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

func (s *Service) CheckSSHPublicKey(ctx ssh.Context, key ssh.PublicKey) bool {
	username := ctx.User()
	b := key.Marshal()
	publicKeyBase64 := Base64Encode(string(b))
	remoteAddr := ctx.RemoteAddr().String()
	authUser, err := s.CheckAuth(username, "", publicKeyBase64, remoteAddr, "T")
	if err != nil {
		return false
	}
	ctx.SetValue("LoginUser", authUser)
	return true

}

func (s *Service) EnsureValidAuth() {

	for i := 0; i < 10; i++ {
		if !s.getProfile() {
			msg := `Connect server error or access key is invalid,
			remove "./data/keys/.access_key" run again`
			log.Error(msg)
			time.Sleep(time.Second * 3)

		} else {
			break
		}
		if i == 3 {
			os.Exit(1)
		}
	}
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

func (s *Service) LoadAccessKey() {
	/*
	   1. 查看配置文件是否包含accessKey，解析不正确则退出程序
	   2. 检查是否已经注册过accessKey，
	   		1）已经注册过则进行解析，解析不正确则退出程序
	   		2）未注册则新注册
	*/
	if s.Conf.CustomerAccessKey != "" {
		fmt.Println(s.Conf.CustomerAccessKey)
		keyAndSecret := strings.Split(s.Conf.CustomerAccessKey, ":")
		if len(keyAndSecret) == 2 {
			s.auth = accessAuth{
				accessKey:    keyAndSecret[0],
				accessSecret: keyAndSecret[1],
			}
		} else {
			fmt.Println("ACCESS_KEY format err")
			os.Exit(1)
		}
		return
	}
	var configPath string

	if !path.IsAbs(s.Conf.AccessKeyFile) {
		configPath = filepath.Join(s.Conf.RootPath, s.Conf.AccessKeyFile)
	} else {
		configPath = s.Conf.AccessKeyFile
	}
	_, err := os.Stat(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Println("Do not have access key, register it!")
			err := s.registerTerminalAndSave()
			if err != nil {
				log.Info("register Failed:", err)
				os.Exit(1)
			}
			return
		} else {
			fmt.Println("sys err:", err)
			os.Exit(1)
		}

	}

	buf, err := ioutil.ReadFile(configPath)
	if err != nil {
		fmt.Println("read Access key Failed:", err)
		os.Exit(1)
	}
	keyAndSecret := strings.Split(string(buf), ":")
	if len(keyAndSecret) == 2 {
		s.auth = accessAuth{
			accessKey:    keyAndSecret[0],
			accessSecret: keyAndSecret[1],
		}
	} else {
		fmt.Println("ACCESS_KEY format err")
		os.Exit(1)
	}

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

func (s *Service) getProfile() bool {

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

func (s *Service) SendHTTPRequest(method, url string, jsonData []byte) ([]byte, error) {
	req, err := http.NewRequest(method, url, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	currentDate := HTTPGMTDate()
	req.Header.Set("Date", currentDate)
	req.Header.Set("Authorization", s.auth.Signature(currentDate))
	resp, err := s.http.Do(req)
	defer resp.Body.Close()
	if err != nil {
		log.Info("Send HTTP Request failed:", err)
		return nil, err
	}
	body, err := ioutil.ReadAll(resp.Body)
	return body, nil
}

func (s *Service) GetUserAssets(uid string) (resp []model.Asset, err error) {

	url := fmt.Sprintf("%s%s", s.Conf.CoreHost, fmt.Sprintf(UserAssetsUrl, uid))

	buf, err := s.SendHTTPRequest("GET", url, nil)
	if err != nil {
		log.Info("get User Assets err:", err)
		return resp, err
	}
	err = json.Unmarshal(buf, &resp)
	if err != nil {
		log.Info(err)
		return resp, err
	}
	return resp, nil

}

func (s *Service) GetUserAssetNodes(uid string) ([]model.AssetNode, error) {

	var resp []model.AssetNode

	url := fmt.Sprintf("%s%s", s.Conf.CoreHost, fmt.Sprintf(UserNodesAssetsUrl, uid))

	buf, err := s.SendHTTPRequest("GET", url, nil)
	if err != nil {
		log.Info("get User Assets Groups err:", err)
		return resp, err
	}
	err = json.Unmarshal(buf, &resp)
	if err != nil {
		log.Info(err)
		return resp, err
	}
	return resp, err
}

func (s *Service) GetSystemUserAssetAuthInfo(systemUserID, assetID string) (authInfo model.SystemUserAuthInfo, err error) {

	url := fmt.Sprintf("%s%s", s.Conf.CoreHost,
		fmt.Sprintf(SystemUserAssetAuthUrl, systemUserID, assetID))
	buf, err := s.SendHTTPRequest("GET", url, nil)
	if err != nil {
		log.Info("get User Assets Groups err:", err)
		return authInfo, err
	}
	err = json.Unmarshal(buf, &authInfo)
	if err != nil {
		log.Info(err)
		return authInfo, err
	}
	return authInfo, err

}

func (s *Service) GetSystemUserAuthInfo(systemUserID string) {

	url := fmt.Sprintf("%s%s", s.Conf.CoreHost,
		fmt.Sprintf(SystemUserAuthUrl, systemUserID))
	buf, err := s.SendHTTPRequest("GET", url, nil)
	if err != nil {
		log.Info("get User Assets Groups err:", err)
		return
	}
	//err = json.Unmarshal(buf, &authInfo)
	fmt.Printf("%s", buf)
	if err != nil {
		log.Info(err)
		return
	}
	return

}

func (s *Service) ValidateUserAssetPermission(userID, systemUserID, AssetID string) bool {
	// cache_policy  0:不使用缓存 1:使用缓存 2: 刷新缓存

	baseUrl, _ := url.Parse(fmt.Sprintf("%s%s", s.Conf.CoreHost, ValidateUserAssetPermission))
	params := url.Values{}
	params.Add("user_id", userID)
	params.Add("asset_id", AssetID)
	params.Add("system_user_id", systemUserID)
	params.Add("cache_policy", "1")

	baseUrl.RawQuery = params.Encode()
	buf, err := s.SendHTTPRequest("GET", baseUrl.String(), nil)
	if err != nil {
		log.Error("Check User Asset Permission err:", err)
		return false
	}
	var res struct {
		Msg bool `json:"msg"'`
	}
	if err = json.Unmarshal(buf, &res); err != nil {
		return false
	}
	return res.Msg
}

func (s *Service) PushSessionReplay(gZipFile, sessionID string) error {
	fp, err := os.Open(gZipFile)
	if err != nil {
		return err
	}
	defer fp.Close()
	fi, err := fp.Stat()
	if err != nil {
		return err
	}

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, err := writer.CreateFormFile("file", fi.Name())
	if err != nil {
		return err
	}
	_, _ = io.Copy(part, fp)
	err = writer.Close() // close writer before POST request
	if err != nil {
		return err
	}

	url := fmt.Sprintf("%s%s", s.Conf.CoreHost, fmt.Sprintf(SessionReplay, sessionID))
	req, err := http.NewRequest("POST", url, body)
	currentDate := HTTPGMTDate()
	req.Header.Add("Content-Type", writer.FormDataContentType())
	req.Header.Set("Date", currentDate)
	req.Header.Set("Authorization", s.auth.Signature(currentDate))
	resp, err := s.http.Do(req)
	defer resp.Body.Close()
	if err != nil {
		log.Info("Send HTTP Request failed:", err)
		return err
	}

	log.Info("PushSessionReplay:", err)
	return err
}

func (s *Service) CreateSession(data []byte) bool {
	url := fmt.Sprintf("%s%s", s.Conf.CoreHost, SessionList)

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(data))
	req.Header.Set("Content-Type", "application/json")
	currentDate := HTTPGMTDate()
	req.Header.Set("Date", currentDate)
	req.Header.Set("Authorization", s.auth.Signature(currentDate))
	resp, err := s.http.Do(req)
	defer resp.Body.Close()
	if err != nil {
		log.Error("create Session err: ", err)
		return false
	}
	if resp.StatusCode == 201 {
		log.Info("create Session 201")
		return true
	}
	return false

}

func (s *Service) FinishSession(id string, jsonData []byte) bool {

	url := fmt.Sprintf("%s%s", s.Conf.CoreHost, fmt.Sprintf(SessionDetail, id))
	res, err := s.SendHTTPRequest("PATCH", url, jsonData)
	fmt.Printf("%s", res)
	if err != nil {
		log.Error(err)
		return false
	}
	return true
}

func (s *Service) FinishReply(id string) bool {
	data := map[string]bool{"has_replay": true}
	jsonData, _ := json.Marshal(data)
	url := fmt.Sprintf("%s%s", s.Conf.CoreHost, fmt.Sprintf(SessionDetail, id))
	_, err := s.SendHTTPRequest("PATCH", url, jsonData)
	if err != nil {
		log.Error(err)
		return false
	}
	return true
}
