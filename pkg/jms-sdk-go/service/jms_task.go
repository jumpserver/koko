package service

import (
	"fmt"
)

func (s *JMService) FinishTask(tid string) error {
	data := map[string]bool{"is_finished": true}
	Url := fmt.Sprintf(FinishTaskURL, tid)
	_, err := s.authClient.Patch(Url, data, nil)
	return err
}
