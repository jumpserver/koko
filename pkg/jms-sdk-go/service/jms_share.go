package service

import (
	"fmt"
	"github.com/jumpserver/koko/pkg/jms-sdk-go/model"
)

func (s *JMService) CreateShareRoom(sessionId string, expired int) (res model.SharingSession, err error) {
	var postData struct {
		Session     string `json:"session"`
		ExpiredTime int    `json:"expired_time"`
	}
	postData.Session = sessionId
	postData.ExpiredTime = expired
	_, err = s.authClient.Post(ShareCreateURL, postData, &res)
	return
}

func (s *JMService) JoinShareRoom(data SharePostData) (res model.ShareRecord, err error) {
	_, err = s.authClient.Post(ShareSessionJoinURL, data, &res)
	return
}

func (s *JMService) FinishShareRoom(recordId string) (err error) {
	reqUrl := fmt.Sprintf(ShareSessionFinishURL, recordId)
	_, err = s.authClient.Patch(reqUrl, nil, nil)
	return
}

type SharePostData struct {
	ShareId    string `json:"sharing"`
	Code       string `json:"verify_code"`
	UserId     string `json:"joiner"`
	RemoteAddr string `json:"remote_addr"`
}
