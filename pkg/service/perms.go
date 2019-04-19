package service

import (
	"cocogo/pkg/model"
	"encoding/json"
	"fmt"
)

func (s *Service) GetUserAssets(uid string) (resp []model.Asset, err error) {

	url := fmt.Sprintf("%s%s", s.Conf.CoreHost, fmt.Sprintf(UserAssetsUrl, uid))

	buf, err := s.SendHTTPRequest("GET", url, nil)
	if err != nil {
		log.Info("get User Assets err:", err)
		return resp, err
	}
	err = json.Unmarshal(buf, &resp)
	if err != nil {
		log.Info(err)
		return resp, err
	}
	return resp, nil

}

func (s *Service) GetUserAssetNodes(uid string) ([]model.AssetNode, error) {

	var resp []model.AssetNode

	url := fmt.Sprintf("%s%s", s.Conf.CoreHost, fmt.Sprintf(UserNodesAssetsUrl, uid))

	buf, err := s.SendHTTPRequest("GET", url, nil)
	if err != nil {
		log.Info("get User Assets Groups err:", err)
		return resp, err
	}
	err = json.Unmarshal(buf, &resp)
	if err != nil {
		log.Info(err)
		return resp, err
	}
	return resp, err
}

func (s *Service) ValidateUserAssetPermission(userID, systemUserID, AssetID string) bool {
	// cache_policy  0:不使用缓存 1:使用缓存 2: 刷新缓存

	baseUrl, _ := neturl.Parse(fmt.Sprintf("%s%s", s.Conf.CoreHost, ValidateUserAssetPermission))
	params := neturl.Values{}
	params.Add("user_id", userID)
	params.Add("asset_id", AssetID)
	params.Add("system_user_id", systemUserID)
	params.Add("cache_policy", "1")

	baseUrl.RawQuery = params.Encode()
	buf, err := s.SendHTTPRequest("GET", baseUrl.String(), nil)
	if err != nil {
		log.Error("Check User Asset Permission err:", err)
		return false
	}
	var res struct {
		Msg bool `json:"msg"'`
	}
	if err = json.Unmarshal(buf, &res); err != nil {
		return false
	}
	return res.Msg
}
