package srvconn

import (
	"sync"

	"github.com/jumpserver/koko/pkg/jms-sdk-go/common"
	"github.com/jumpserver/koko/pkg/jms-sdk-go/model"
	"github.com/jumpserver/koko/pkg/jms-sdk-go/service"
	"github.com/jumpserver/koko/pkg/logger"
	"github.com/jumpserver/koko/pkg/session"
)

type SftpSession struct {
	*SftpConn
	sess       *model.Session
	once       sync.Once
	jmsService *service.JMService
}

func (s *SftpSession) CloseWithReason(reason model.SessionLifecycleReasonErr) {
	s.once.Do(func() {
		s.SftpConn.Close()
		session.RemoveSessionById(s.sess.ID)
		if err := s.jmsService.SessionFinished(s.sess.ID, common.NewNowUTCTime()); err != nil {
			logger.Errorf("SFTP Session finished err: %s", err)
		}
		logger.Debugf("SFTP Session finished %s", s.sess.ID)
		logObj := model.SessionLifecycleLog{Reason: reason.String()}
		if err := s.jmsService.RecordSessionLifecycleLog(s.sess.ID, model.AssetConnectFinished, logObj); err != nil {
			logger.Errorf("Update session %s lifecycle asset_connect_finished failed: %s", s.sess.ID, err)
		}
	})

}

func (s *SftpSession) Close() {
	s.CloseWithReason(model.ReasonErrConnectDisconnect)
}
