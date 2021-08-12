package service

import (
	"fmt"
	"github.com/jumpserver/koko/pkg/jms-sdk-go/model"
)

func (s *JMService) GetMySQLApplicationById(appId string) (app model.DatabaseApplication, err error) {
	err = s.getApplicationById(appId, &app)
	return
}

func (s *JMService) GetMySQLOrMariadbApplicationById(appId string) (app model.DatabaseApplication, err error) {
	err = s.getApplicationById(appId, &app)
	return
}

func (s *JMService) GetK8sApplicationById(appId string) (app model.K8sApplication, err error) {
	err = s.getApplicationById(appId, &app)
	return
}

func (s *JMService) getApplicationById(appId string, res interface{}) error {
	reqUrl := fmt.Sprintf(ApplicationDetailURL, appId)
	_, err := s.authClient.Get(reqUrl, res)
	return err
}

func (s *JMService) GetUserApplicationAuthInfo(systemUserID, appID, userID, username string) (info model.SystemUserAuthInfo, err error) {
	Url := fmt.Sprintf(SystemUserAppAuthURL, systemUserID, appID)
	params := make(map[string]string)
	if username != "" {
		params["username"] = username
	}
	if userID != "" {
		params["user_id"] = userID
	}
	_, err = s.authClient.Get(Url, &info, params)
	return
}

func (s *JMService) GetUserApplicationSystemUsers(userId, appId string) (res []model.SystemUser, err error) {
	reqUrl := fmt.Sprintf(UserPermsApplicationSystemUsersURL, userId, appId)
	_, err = s.authClient.Get(reqUrl, &res)
	return
}
