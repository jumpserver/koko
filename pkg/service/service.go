package service

//func (s *Service) EnsureValidAuth() {
//	for i := 0; i < 10; i++ {
//		if !s.validateAuth() {
//			msg := `Connect server error or access key is invalid,
//			remove "./data/keys/.access_key" run again`
//			logger.Error(msg)
//			time.Sleep(time.Second * 3)
//
//		} else {
//			break
//		}
//		if i == 3 {
//			os.Exit(1)
//		}
//	}
//}
//
//func (s *Service) validateAuth() bool {
//
//	url := fmt.Sprintf("%s%s", s.Conf.CoreHost, UserProfileUrl)
//	body, err := s.SendHTTPRequest(http.MethodGet, url, nil)
//	if err != nil {
//		log.Info("Read response Body err:", err)
//		return false
//	}
//	result := model.User{}
//	err = json.Unmarshal(body, &result)
//	if err != nil {
//		log.Info("json.Unmarshal", err)
//		return false
//	}
//	log.Info(result)
//	return result != model.User{}
//}

//func (s *Service) registerTerminalAndSave() error {
//
//	postMap := map[string]string{
//		"name":    s.Conf.Name,
//		"comment": s.Conf.Comment,
//	}
//	data, err := json.Marshal(postMap)
//	if err != nil {
//		log.Info("json encode failed:", err)
//		return err
//
//	}
//	url := fmt.Sprintf("%s%s", s.Conf.CoreHost, TerminalRegisterUrl)
//	req, err := http.NewRequest("POST", url, bytes.NewBuffer(data))
//	if err != nil {
//		log.Info("http NewRequest err:", err)
//		return err
//	}
//	req.Header.Set("Content-Type", "application/json")
//	req.Header.Set("Authorization", fmt.Sprintf("BootstrapToken %s", s.Conf.BootstrapToken))
//	resp, err := s.http.Do(req)
//	if err != nil {
//		log.Info("http request err:", err)
//		return err
//
//	}
//	defer resp.Body.Close()
//	body, err := ioutil.ReadAll(resp.Body)
//	if err != nil {
//		log.Info("read resp body err:", err)
//		return err
//	}
//	/*
//		{
//				    "name": "sss2",
//				    "comment": "Coco",
//				    "service_account": {
//				        "id": "c2dece80-1811-42bc-bd5b-aef0f4180263",
//				        "name": "sss2",
//				        "access_key": {
//				            "id": "f9b2cf91-7f30-45ea-9edf-b73ec0f48d5a",
//				            "secret": "fd083b6c-e823-47bf-870c-0dd6051e69f1"
//				        }
//				    }
//				}
//	*/
//	log.Infof("%s", body)
//
//	var resBody struct {
//		ServiceAccount struct {
//			Id        string `json:"id"`
//			Name      string `json:"name"`
//			Accesskey struct {
//				Id     string `json:"id"`
//				Secret string `json:"secret"`
//			} `json:"access_key"`
//		} `json:"service_account"`
//	}
//
//	err = json.Unmarshal(body, &resBody)
//	if err != nil {
//		log.Info("json Unmarshal:", err)
//		return err
//	}
//	if resBody.ServiceAccount.Name == "" {
//		return errors.New(string(body))
//	}
//
//	s.auth = accessAuth{
//		accessKey:    resBody.ServiceAccount.Accesskey.Id,
//		accessSecret: resBody.ServiceAccount.Accesskey.Secret,
//	}
//	return s.saveAccessKey()
//}
//
//func (s *Service) saveAccessKey() error {
//	MakeSureDirExit(s.Conf.AccessKeyFile)
//	f, err := os.Create(s.Conf.AccessKeyFile)
//	fmt.Println("Create file path:", s.Conf.AccessKeyFile)
//	if err != nil {
//		return err
//	}
//	keyAndSecret := fmt.Sprintf("%s:%s", s.auth.accessKey, s.auth.accessSecret)
//	_, err = f.WriteString(keyAndSecret)
//	if err != nil {
//		return err
//	}
//	err = f.Close()
//	if err != nil {
//		return err
//	}
//	return nil
//}
