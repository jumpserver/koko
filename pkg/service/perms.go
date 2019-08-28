package service

import (
	"fmt"
	"strconv"

	"github.com/jumpserver/koko/pkg/logger"
	"github.com/jumpserver/koko/pkg/model"
)

func GetUserAssets(userID, search string, pageSize, offset int) (resp model.AssetsPaginationResponse) {
	if pageSize < 0 {
		pageSize = 0
	}
	params := map[string]string{
		"search": search,
		"limit":  strconv.Itoa(pageSize),
		"offset": strconv.Itoa(offset),
	}

	Url := fmt.Sprintf(UserAssetsURL, userID)
	var err error
	if pageSize > 0 {
		_, err = authClient.Get(Url, &resp, params)
	} else {
		var data model.AssetList
		_, err = authClient.Get(Url, &data, params)
		resp.Data = data
	}
	if err != nil {
		logger.Error("Get user assets error: ", err)
	}
	return
}

func GetUserAllAssets(userID string) (assets []model.Asset) {
	Url := fmt.Sprintf(UserAssetsURL, userID)
	_, err := authClient.Get(Url, &assets)
	if err != nil {
		logger.Error("Get user all assets error: ", err)
	}
	return
}

func GetUserAssetByID(userID, assertID string) (assets []model.Asset) {
	params := map[string]string{
		"id": assertID,
	}
	Url := fmt.Sprintf(UserAssetsURL, userID)
	_, err := authClient.Get(Url, &assets, params)
	if err != nil {
		logger.Error("Get user asset by ID error: ", err)
	}
	return
}

func GetUserNodes(userID, cachePolicy string) (nodes model.NodeList) {
	if cachePolicy == "" {
		cachePolicy = "1"
	}
	payload := map[string]string{"cache_policy": cachePolicy}
	Url := fmt.Sprintf(UserNodesListURL, userID)
	_, err := authClient.Get(Url, &nodes, payload)
	if err != nil {
		logger.Error("Get user nodes error: ", err)
	}
	return
}

func GetUserAssetSystemUsers(userID, assetID string) (sysUsers []model.SystemUser) {
	Url := fmt.Sprintf(UserAssetSystemUsersURL, userID, assetID)
	_, err := authClient.Get(Url, &sysUsers)
	if err != nil {
		logger.Error("Get user asset system users error: ", err)
	}
	return
}

func GetUserNodeAssets(userID, nodeID, cachePolicy string) (assets model.AssetList) {
	if cachePolicy == "" {
		cachePolicy = "1"
	}
	payload := map[string]string{"cache_policy": cachePolicy, "all": "1"}
	Url := fmt.Sprintf(UserNodeAssetsListURL, userID, nodeID)
	_, err := authClient.Get(Url, &assets, payload)
	if err != nil {
		logger.Error("Get user node assets error: ", err)
		return
	}
	return
}

func ValidateUserAssetPermission(userID, assetID, systemUserID, action string) bool {
	payload := map[string]string{
		"user_id":        userID,
		"asset_id":       assetID,
		"system_user_id": systemUserID,
		"action_name":    action,
		"cache_policy":   "1",
	}
	Url := ValidateUserAssetPermissionURL
	var res struct {
		Msg bool `json:"msg"`
	}
	_, err := authClient.Get(Url, &res, payload)

	if err != nil {
		logger.Error(err)
		return false
	}

	return res.Msg
}
