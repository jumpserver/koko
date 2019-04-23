package sdk

//
//func (s *Service) PushSessionReplay(gZipFile, sessionID string) error {
//	fp, err := os.Open(gZipFile)
//	if err != nil {
//		return err
//	}
//	defer fp.Close()
//	fi, err := fp.Stat()
//	if err != nil {
//		return err
//	}
//
//	body := &bytes.Buffer{}
//	writer := multipart.NewWriter(body)
//	part, err := writer.CreateFormFile("file", fi.Name())
//	if err != nil {
//		return err
//	}
//	_, _ = io.Copy(part, fp)
//	err = writer.Close() // close writer before POST request
//	if err != nil {
//		return err
//	}
//
//	url := fmt.Sprintf("%s%s", s.Conf.CoreHost, fmt.Sprintf(SessionReplay, sessionID))
//	req, err := http.NewRequest("POST", url, body)
//	currentDate := HTTPGMTDate()
//	req.Header.Add("Content-Type", writer.FormDataContentType())
//	req.Header.Set("Date", currentDate)
//	req.Header.Set("Authorization", s.auth.Signature(currentDate))
//	resp, err := s.http.Do(req)
//	defer resp.Body.Close()
//	if err != nil {
//		log.Info("Send HTTP Request failed:", err)
//		return err
//	}
//
//	log.Info("PushSessionReplay:", err)
//	return err
//}
//
//func (s *Service) CreateSession(data []byte) bool {
//	url := fmt.Sprintf("%s%s", s.Conf.CoreHost, SessionList)
//
//	req, err := http.NewRequest("POST", url, bytes.NewBuffer(data))
//	req.Header.Set("Content-Type", "application/json")
//	currentDate := HTTPGMTDate()
//	req.Header.Set("Date", currentDate)
//	req.Header.Set("Authorization", s.auth.Signature(currentDate))
//	resp, err := s.http.Do(req)
//	defer resp.Body.Close()
//	if err != nil {
//		log.Error("create Session err: ", err)
//		return false
//	}
//	if resp.StatusCode == 201 {
//		log.Info("create Session 201")
//		return true
//	}
//	return false
//
//}
//
//func (s *Service) FinishSession(id string, jsonData []byte) bool {
//
//	url := fmt.Sprintf("%s%s", s.Conf.CoreHost, fmt.Sprintf(SessionDetail, id))
//	res, err := s.SendHTTPRequest("PATCH", url, jsonData)
//	fmt.Printf("%s", res)
//	if err != nil {
//		log.Error(err)
//		return false
//	}
//	return true
//}
//
//func (s *Service) FinishReply(id string) bool {
//	data := map[string]bool{"has_replay": true}
//	jsonData, _ := json.Marshal(data)
//	url := fmt.Sprintf("%s%s", s.Conf.CoreHost, fmt.Sprintf(SessionDetail, id))
//	_, err := s.SendHTTPRequest("PATCH", url, jsonData)
//	if err != nil {
//		log.Error(err)
//		return false
//	}
//	return true
//}
//
//func (s *Service) LoadTerminalConfig() {
//	url := fmt.Sprintf("%s%s", s.Conf.CoreHost, TerminalConfigUrl)
//	req, err := http.NewRequest(http.MethodGet, url, nil)
//	if err != nil {
//		log.Info(err)
//	}
//	currentDate := HTTPGMTDate()
//	req.Header.Set("Content-Type", "application/json")
//	req.Header.Set("Date", currentDate)
//	req.Header.Set("Authorization", s.auth.Signature(currentDate))
//	resp, err := s.http.Do(req)
//	if err != nil {
//		log.Info("client http request failed:", err)
//	}
//
//	defer resp.Body.Close()
//	body, err := ioutil.ReadAll(resp.Body)
//	if err != nil {
//		log.Info("Read response Body err:", err)
//		return
//	}
//	fmt.Printf("%s\n", body)
//	resBody := config.TerminalConfig{}
//	err = json.Unmarshal(body, &resBody)
//	if err != nil {
//		log.Info("json.Unmarshal", err)
//		return
//	}
//	s.Conf.TermConfig = &resBody
//	fmt.Println(resBody)
//
//}
