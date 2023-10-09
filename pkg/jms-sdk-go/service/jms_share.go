package service

import (
	"fmt"
	"strings"

	"github.com/jumpserver/koko/pkg/jms-sdk-go/model"
)

func (s *JMService) CreateShareRoom(data model.SharingSessionRequest) (res model.SharingSession, err error) {
	_, err = s.authClient.Post(ShareCreateURL, data, &res)
	return
}

func (s *JMService) GetShareUserInfo(query string) (res []*model.MiniUser, err error) {
	params := make(map[string]string)
	params["action"] = "suggestion"
	params["search"] = query
	_, err = s.authClient.Get(UserListURL, &res, params)
	return
}

func (s *JMService) JoinShareRoom(data model.SharePostData) (res model.ShareRecord, err error) {
	_, err = s.authClient.Post(ShareSessionJoinURL, data, &res)
	return
}

func (s *JMService) FinishShareRoom(recordId string) (err error) {
	reqUrl := fmt.Sprintf(ShareSessionFinishURL, recordId)
	_, err = s.authClient.Patch(reqUrl, nil, nil)
	return
}

func (s *JMService) SyncUserKokoPreference(cookies map[string]string, data model.UserKokoPreference) (err error) {
	/*
		csrfToken 存储在 cookies 中
		其 使用的名称 name 为 `{SESSION_COOKIE_NAME_PREFIX}csrftoken` 动态组成
	*/
	var (
		csrfToken  string
		namePrefix string
	)
	checkNamePrefixValid := func(name string) bool {
		invalidStrings := []string{`""`, `''`}
		for _, invalidString := range invalidStrings {
			if strings.Index(name, invalidString) != -1 {
				return false
			}
		}
		return true
	}
	namePrefix = cookies["SESSION_COOKIE_NAME_PREFIX"]
	csrfCookieName := "csrftoken"
	if namePrefix != "" && checkNamePrefixValid(namePrefix) {
		csrfCookieName = namePrefix + csrfCookieName
	}
	csrfToken = cookies[csrfCookieName]
	client := s.authClient.Clone()
	client.SetHeader("X-CSRFToken", csrfToken)
	for k, v := range cookies {
		client.SetCookie(k, v)
	}
	_, err = client.Patch(UserKoKoPreferenceURL, data, nil)
	return
}
