package service

import (
	"encoding/json"
	"fmt"

	"cocogo/pkg/model"
)

func (s *Service) GetSystemUserAssetAuthInfo(systemUserID, assetID string) (authInfo model.SystemUserAuthInfo, err error) {

	url := fmt.Sprintf("%s%s", s.Conf.CoreHost,
		fmt.Sprintf(SystemUserAssetAuthUrl, systemUserID, assetID),
	)
	buf, err := s.SendHTTPRequest("GET", url, nil)
	if err != nil {
		log.Info("get User Assets Groups err:", err)
		return authInfo, err
	}
	err = json.Unmarshal(buf, &authInfo)
	if err != nil {
		log.Info(err)
		return authInfo, err
	}
	return authInfo, err

}

func (s *Service) GetSystemUserAuthInfo(systemUserID string) {

	url := fmt.Sprintf("%s%s", s.Conf.CoreHost,
		fmt.Sprintf(SystemUserAuthUrl, systemUserID))
	buf, err := s.SendHTTPRequest("GET", url, nil)
	if err != nil {
		log.Info("get User Assets Groups err:", err)
		return
	}
	//err = json.Unmarshal(buf, &authInfo)
	fmt.Printf("%s", buf)
	if err != nil {
		log.Info(err)
		return
	}
	return

}
