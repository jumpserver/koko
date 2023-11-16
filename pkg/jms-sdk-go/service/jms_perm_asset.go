package service

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/jumpserver/koko/pkg/jms-sdk-go/model"
)

func (s *JMService) SearchPermAsset(userId, key string) (res model.PermAssetList, err error) {
	reqUrl := fmt.Sprintf(UserPermsAssetsURL, userId)
	payload := map[string]string{"search": key}
	_, err = s.authClient.Get(reqUrl, &res, payload)
	return
}

func (s *JMService) GetUserPermAssetDetailById(userId, assetId string) (resp model.PermAssetDetail, err error) {
	reqUrl := fmt.Sprintf(UserPermsAssetAccountsURL, userId, assetId)
	_, err = s.authClient.Get(reqUrl, &resp)
	return
}

func (s *JMService) GetAllUserPermsAssets(userId string) ([]model.PermAsset, error) {
	var params model.PaginationParam
	res, err := s.GetUserPermsAssets(userId, params)
	if err != nil {
		return nil, err
	}
	return res.Data, nil
}

func (s *JMService) GetUserPermsAssets(userId string, params model.PaginationParam) (resp model.PaginationResponse, err error) {
	reqUrl := fmt.Sprintf(UserPermsAssetsURL, userId)
	return s.getPaginationAssets(reqUrl, params)
}

func (s *JMService) RefreshUserAllPermsAssets(userId string) ([]model.PermAsset, error) {
	var params model.PaginationParam
	params.Refresh = true
	res, err := s.GetUserPermsAssets(userId, params)
	if err != nil {
		return nil, err
	}
	return res.Data, nil
}

func (s *JMService) GetUserAssetByID(userId, assetId string) (assets []model.PermAsset, err error) {
	params := map[string]string{
		"id": assetId,
	}
	reqUrl := fmt.Sprintf(UserPermsAssetsURL, userId)
	_, err = s.authClient.Get(reqUrl, &assets, params)
	return
}

func (s *JMService) GetUserPermAssetsByIP(userId, assetIP string) (assets []model.PermAsset, err error) {
	params := map[string]string{
		"address": assetIP,
	}
	reqUrl := fmt.Sprintf(UserPermsAssetsURL, userId)
	_, err = s.authClient.Get(reqUrl, &assets, params)
	return
}

func (s *JMService) GetUserPermAssetById(userId, assetId string) (assets []model.PermAsset, err error) {
	params := map[string]string{
		"id": assetId,
	}
	reqUrl := fmt.Sprintf(UserPermsAssetsURL, userId)
	_, err = s.authClient.Get(reqUrl, &assets, params)
	return
}

func (s *JMService) getPaginationAssets(reqUrl string, param model.PaginationParam) (resp model.PaginationResponse, err error) {
	if param.PageSize < 0 {
		param.PageSize = 0
	}
	paramsArray := make([]map[string]string, 0, len(param.Searches)+2)
	for i := 0; i < len(param.Searches); i++ {
		paramsArray = append(paramsArray, map[string]string{
			"search": strings.TrimSpace(param.Searches[i]),
		})
	}

	params := map[string]string{
		"limit":  strconv.Itoa(param.PageSize),
		"offset": strconv.Itoa(param.Offset),
	}
	if param.Refresh {
		params["rebuild_tree"] = "1"
	}
	if param.Order != "" {
		params["order"] = param.Order
	}
	if param.Type != "" {
		params["type"] = param.Type
	}
	if param.Category != "" {
		params["category"] = param.Category
	}

	if param.IsActive {
		params["is_active"] = "true"
	}
	if param.Protocols != nil {
		params["protocols"] = strings.Join(param.Protocols, ",")
	}

	paramsArray = append(paramsArray, params)
	if param.PageSize > 0 {
		_, err = s.authClient.Get(reqUrl, &resp, paramsArray...)
	} else {
		var data []model.PermAsset
		_, err = s.authClient.Get(reqUrl, &data, paramsArray...)
		resp.Data = data
		resp.Total = len(data)
	}
	return
}
