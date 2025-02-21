package service

type JoinFaceMonitorRequest struct {
	FaceMonitorToken string `json:"face_monitor_token"`
	SessionId        string `json:"session_id"`
}

func (s *JMService) JoinFaceMonitor(result JoinFaceMonitorRequest) error {
	var resp = map[string]interface{}{}
	if _, err := s.authClient.Post(FaceMonitorContextUrl, &result, &resp); err != nil {
		return err
	}
	return nil
}
