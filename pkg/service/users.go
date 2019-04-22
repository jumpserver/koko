package service

//
//func (s *Service) CheckAuth(username, password, publicKey, remoteAddr, loginType string) (model.User, error) {
//	/*
//		{
//		'token': '0191970b1f5b414bbae42ec8fbb2a2ad',
//		'user':{'id': '34987591-bf75-4e5f-a102-6d59a1103431',
//			'name': 'softwareuser1', 'username': 'softwareuser1',
//			'email': 'xplz@hotmail.com',
//			'groups': ['bdc861f9-f476-4554-9bd4-13c3112e469d'],
//			'groups_display': '研发组', 'role': 'User',
//			'role_display': '用户', 'avatar_url': '/static/img/avatar/user.png',
//			'wechat': '', 'phone': None, 'otp_level': 0, 'comment': '',
//			'source': 'local', 'source_display': 'Local', 'is_valid': True,
//			'is_expired': False, 'is_active': True, 'created_by': 'admin',
//			'is_first_login': True, 'date_password_last_updated': '2019-03-08 11:47:04 +0800',
//			'date_expired': '2089-02-18 09:37:00 +0800'}}
//	*/
//
//	postMap := map[string]string{
//		"username":    username,
//		"password":    password,
//		"public_key":  publicKey,
//		"remote_addr": remoteAddr,
//		"login_type":  loginType,
//	}
//
//	data, err := json.Marshal(postMap)
//	if err != nil {
//		log.Info(err)
//		return model.User{}, err
//	}
//
//	url := fmt.Sprintf("%s%s", s.Conf.CoreHost, UserAuthUrl)
//	body, err := s.SendHTTPRequest(http.MethodPost, url, data)
//
//	if err != nil {
//		log.Info("read body failed:", err)
//		return model.User{}, err
//	}
//	var result struct {
//		Token string     `json:"token"`
//		User  model.User `json:"user"`
//	}
//
//	err = json.Unmarshal(body, &result)
//	if err != nil {
//		log.Info("json decode failed:", err)
//		return model.User{}, err
//	}
//
//	return result.User, nil
//}
//
//func (s *Service) CheckSSHPassword(ctx ssh.Context, password string) bool {
//
//	username := ctx.User()
//	remoteAddr := ctx.RemoteAddr().String()
//	authUser, err := s.CheckAuth(username, password, "", remoteAddr, "T")
//	if err != nil {
//		return false
//	}
//	ctx.SetValue("LoginUser", authUser)
//	return true
//}
