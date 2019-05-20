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
	err := client.Post(TerminalRegisterURL, data, &res)
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
	err := authClient.Post(SessionListURL, data, &res)
	if err == nil {
		return true
	}
	logger.Error(err)
	return false
}

func FinishSession(data map[string]interface{}) {

	var res map[string]interface{}
	if sid, ok := data["id"]; ok {
		playborad := map[string]interface{}{
			"is_finished": true,
			"date_end":    data["date_end"],
		}
		Url := fmt.Sprintf(SessionDetailURL, sid)
		err := authClient.Patch(Url, playborad, &res)
		if err != nil {
			logger.Error(err)
		}
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
	err := authClient.Patch(Url, data, &res)
	if err != nil {
		logger.Error(err)
		return false
	}
	return true
}

func PushSessionReplay(sessionID, gZipFile string) (err error) {
	var res map[string]interface{}
	Url := fmt.Sprintf(SessionReplayURL, sessionID)
	err = authClient.UploadFile(Url, gZipFile, &res)
	if err != nil {
		logger.Error(err)
	}
	return
}

func PushSessionCommand(commands []*model.Command) (err error) {
	err = authClient.Post(SessionCommandURL, commands, nil)
	if err != nil {
		logger.Error(err)
	}
	return
}
