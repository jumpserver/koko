package service

import (
	"fmt"

	"github.com/jumpserver/koko/pkg/jms-sdk-go/model"
)

func (s *JMService) RecordSessionLifecycleLog(sid string, event model.LifecycleEvent, logObj model.SessionLifecycleLog) (err error) {
	data := map[string]interface{}{
		"event": event,
	}
	if logObj.Reason != "" {
		data["reason"] = logObj.Reason
	}
	if logObj.User != "" {
		data["user"] = logObj.User
	}

	reqURL := fmt.Sprintf(SessionLifecycleLogURL, sid)
	var resp map[string]interface{}
	_, err = s.authClient.Post(reqURL, data, &resp)
	return
}
