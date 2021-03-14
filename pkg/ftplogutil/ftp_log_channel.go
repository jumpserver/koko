package ftplogutil

import (
	"github.com/jumpserver/koko/pkg/model"
)

var LogReadyChan = make(chan model.FTPLog)
var FileReadyChan = make(chan model.FTPLog)

func SendNotifyFtpLog(data model.FTPLog) {
	LogReadyChan <- data
}
func SendNotifyFileReady(data model.FTPLog) {
	FileReadyChan <- data
}
