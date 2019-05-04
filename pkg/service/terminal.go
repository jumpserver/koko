package service

import (
	"fmt"

	"cocogo/pkg/logger"
	"cocogo/pkg/model"
)

func RegisterTerminal(name, token, comment string) (res model.Terminal) {
	if client.Headers == nil {
		client.Headers = make(map[string]string)
	}
	client.Headers["Authorization"] = fmt.Sprintf("BootstrapToken %s", token)
	data := map[string]string{"name": name, "comment": comment}
	Url := client.ParseUrlQuery(TerminalRegisterURL, nil)
	err := client.Post(Url, data, &res)
	if err != nil {
		logger.Error(err)
	}
	return
}

func getTerminalProfile() (user model.User) {
	Url := authClient.ParseUrlQuery(UserProfileURL, nil)

	err := authClient.Get(Url, &user)
	if err != nil {
		logger.Error(err)
	}
	return
}

func TerminalHeartBeat(sIds []string) (res []model.TerminalTask) {

	data := map[string][]string{
		"sessions": sIds,
	}
	err := authClient.Post(TerminalHeartBeatURL, data, &res)
	if err != nil {
		logger.Error(err)
	}
	return
}

func CreateSession(data map[string]interface{}) bool {
	var res map[string]interface{}
	Url := authClient.ParseUrlQuery(SessionListURL, nil)
	err := authClient.Post(Url, data, &res)
	if err == nil {
		return true
	}
	logger.Error(err)
	return false
}

func FinishSession(sid, dataEnd string) {
	var res map[string]interface{}
	data := map[string]interface{}{
		"is_finished": true,
		"date_end":    dataEnd,
	}
	Url := fmt.Sprintf(SessionDetailURL, sid)
	err := authClient.Patch(Url, data, &res)
	if err != nil {
		logger.Error(err)
	}
}

func FinishReply(sid string) bool {
	var res map[string]interface{}
	data := map[string]bool{"has_replay": true}
	Url := fmt.Sprintf(SessionDetailURL, sid)
	err := authClient.Patch(Url, data, &res)
	if err != nil {
		logger.Error(err)
		return false
	}
	return true
}

func FinishTask(tid string) bool {
	var res map[string]interface{}
	data := map[string]bool{"is_finished": true}
	Url := fmt.Sprintf(FinishTaskURL, tid)
	err := authClient.Patch(Url, data, res)
	if err != nil {
		logger.Error(err)
		return false
	}
	return true
}

func LoadConfigFromServer() (res model.TerminalConf) {
	err := authClient.Get(TerminalConfigURL, &res)
	if err != nil {
		logger.Error(err)
	}
	return
}

func PushSessionReplay(sessionID, gZipFile string) {

}
