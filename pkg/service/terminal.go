package service

import (
	"fmt"
	"github.com/jumpserver/koko/pkg/utils"

	"github.com/jumpserver/koko/pkg/logger"
	"github.com/jumpserver/koko/pkg/model"
)

func RegisterTerminal(name, token, comment string) (res model.Terminal) {
	client := newClient()
	client.Headers["Authorization"] = fmt.Sprintf("BootstrapToken %s", token)
	data := map[string]string{"name": name, "comment": comment, "type": "koko"}
	_, err := client.Post(TerminalRegisterURL, data, &res)
	if err != nil {
		logger.Error(err)
	}
	return
}

type HeartbeatData struct {
	SessionOnlineIds []string `json:"sessions"`
	SessionOnline    int      `json:"session_online"`
	CpuUsed          float64  `json:"cpu_load"`
	MemoryUsed       float64  `json:"memory_used"`
	DiskUsed         float64  `json:"disk_used"`
}

func TerminalHeartBeat(sIds []string) (res []model.TerminalTask) {
	data := HeartbeatData{
		SessionOnlineIds: sIds,
		CpuUsed:          utils.CpuLoad1Usage(),
		MemoryUsed:       utils.MemoryUsagePercent(),
		DiskUsed:         utils.DiskUsagePercent(),
		SessionOnline:    len(sIds),
	}
	_, err := authClient.Post(TerminalHeartBeatURL, data, &res)
	if err != nil {
		logger.Error(err)
	}
	return
}

func CreateSession(data map[string]interface{}) bool {
	var res map[string]interface{}
	_, err := authClient.Post(SessionListURL, data, &res)
	if err == nil {
		return true
	}
	logger.Error(err)
	return false
}

func FinishSession(data map[string]interface{}) {

	var res map[string]interface{}
	if sid, ok := data["id"]; ok {
		payload := map[string]interface{}{
			"is_finished": true,
			"date_end":    data["date_end"],
		}
		Url := fmt.Sprintf(SessionDetailURL, sid)
		_, err := authClient.Patch(Url, payload, &res)
		if err != nil {
			logger.Error(err)
		}
	}

}

func FinishReply(sid string) bool {
	var res map[string]interface{}
	data := map[string]bool{"has_replay": true}
	Url := fmt.Sprintf(SessionDetailURL, sid)
	_, err := authClient.Patch(Url, data, &res)
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
	_, err := authClient.Patch(Url, data, &res)
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
	_, err = authClient.Post(SessionCommandURL, commands, nil)
	if err != nil {
		logger.Error(err)
	}
	return
}

func PushFTPLog(data *model.FTPLog) (err error) {
	_, err = authClient.Post(FTPLogListURL, data, nil)
	if err != nil {
		logger.Error(err)
	}
	return
}

func PushFTPLogFile(ftpLogId, gZipFile string) (err error) {
	var res map[string]interface{}
	Url := fmt.Sprintf(FTPLogFileURL, ftpLogId)
	err = authClient.UploadFile(Url, gZipFile, &res)
	if err != nil {
		logger.Error(err)
	}
	return
}

func FinishFTPLogFileUpload(sid string) bool {
	var res map[string]interface{}
	data := map[string]bool{"has_file_record": true}
	Url := fmt.Sprintf(FTPLogUpdateURL, sid)
	_, err := authClient.Patch(Url, data, &res)
	if err != nil {
		logger.Error(err)
		return false
	}
	return true
}

func FTPLogFailed(sid string) bool {
	var res map[string]interface{}
	data := map[string]bool{"is_success": false}
	Url := fmt.Sprintf(FTPLogUpdateURL, sid)
	_, err := authClient.Patch(Url, data, &res)
	if err != nil {
		logger.Error(err)
		return false
	}
	return true
}

func JoinRoomValidate(userID, sessionID string) bool {
	data := map[string]string{
		"session_id": sessionID,
		"user_id":    userID,
	}
	var result struct {
		Ok  bool   `json:"ok"`
		Msg string `json:"msg"`
	}
	_, err := authClient.Post(JoinRoomValidateURL, data, &result)
	if err != nil {
		logger.Errorf("Validate join room err: %s", err)
		return false
	}
	if !result.Ok && result.Msg != "" {
		logger.Errorf("Validate result err msg: %s", result.Msg)
	}

	return result.Ok
}
