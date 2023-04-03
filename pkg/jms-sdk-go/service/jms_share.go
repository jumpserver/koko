package service

import (
	"fmt"
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
