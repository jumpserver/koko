package service

import (
	"fmt"
	"github.com/jumpserver/koko/pkg/jms-sdk-go/common"
	"github.com/jumpserver/koko/pkg/jms-sdk-go/model"
)

func (s *JMService) Upload(sessionID, gZipFile string) error {
	version := model.ParseReplayVersion(gZipFile, model.Version3)
	return s.UploadReplay(sessionID, gZipFile, version)
}

func (s *JMService) UploadReplay(sid, gZipFile string, version model.ReplayVersion) error {
	var res map[string]interface{}
	Url := fmt.Sprintf(SessionReplayURL, sid)
	fields := make(map[string]string)
	fields["version"] = string(version)
	return s.authClient.PostFileWithFields(Url, gZipFile, fields, &res)
}

func (s *JMService) FinishReply(sid string) error {
	data := map[string]bool{"has_replay": true}
	return s.sessionPatch(sid, data)
}

func (s *JMService) CreateSession(sess model.Session) error {
	_, err := s.authClient.Post(SessionListURL, sess, nil)
	return err
}

func (s *JMService) SessionSuccess(sid string) error {
	data := map[string]bool{
		"is_success": true,
	}
	return s.sessionPatch(sid, data)
}

func (s *JMService) SessionFailed(sid string, err error) error {
	data := map[string]bool{
		"is_success": false,
	}
	return s.sessionPatch(sid, data)
}
func (s *JMService) SessionDisconnect(sid string) error {
	return s.SessionFinished(sid, common.NewNowUTCTime())
}

func (s *JMService) SessionFinished(sid string, time common.UTCTime) error {
	data := map[string]interface{}{
		"is_finished": true,
		"date_end":    time,
	}
	return s.sessionPatch(sid, data)
}

func (s *JMService) sessionPatch(sid string, data interface{}) error {
	Url := fmt.Sprintf(SessionDetailURL, sid)
	_, err := s.authClient.Patch(Url, data, nil)
	return err
}

func (s *JMService) GetSessionById(sid string) (data model.Session, err error) {
	reqURL := fmt.Sprintf(SessionDetailURL, sid)
	_, err = s.authClient.Get(reqURL, &data)
	return
}
