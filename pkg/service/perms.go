package service

import (
	"fmt"
	"net/url"
	"strconv"

	"github.com/jumpserver/koko/pkg/logger"
	"github.com/jumpserver/koko/pkg/model"
)

func GetUserAssets(userID string, pageSize, offset int, searches ...string) (resp model.AssetsPaginationResponse) {
	if pageSize < 0 {
		pageSize = 0
	}
	paramsArray := make([]map[string]string, 0, len(searches)+2)
	for i := 0; i < len(searches); i++ {
		paramsArray = append(paramsArray, map[string]string{
			"search": searches[i],
		})
	}
	params := map[string]string{
		"limit":  strconv.Itoa(pageSize),
		"offset": strconv.Itoa(offset),
	}
	paramsArray = append(paramsArray, params)
	Url := fmt.Sprintf(UserAssetsURL, userID)
	var err error
	if pageSize > 0 {
		_, err = authClient.Get(Url, &resp, paramsArray...)
	} else {
		var data model.AssetList
		_, err = authClient.Get(Url, &data, paramsArray...)
		resp.Data = data
		resp.Total = len(data)
	}
	if err != nil {
		logger.Error("Get user assets error: ", err)
	}
	return
}

func ForceRefreshUserPemAssets(userID string) error {
	params := map[string]string{
		"limit":  "1",
		"offset": "0",
		"cache":  "2",
	}
	Url := fmt.Sprintf(UserAssetsURL, userID)
	var resp model.AssetsPaginationResponse
	_, err := authClient.Get(Url, &resp, params)
	if err != nil {
		logger.Errorf("Refresh user assets error: %s", err)
	}
	return err
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

func GetUserNodePaginationAssets(userID, nodeID string, pageSize, offset int, searches ...string) (resp model.AssetsPaginationResponse) {
	if pageSize < 0 {
		pageSize = 0
	}
	paramsArray := make([]map[string]string, 0, len(searches)+2)
	for i := 0; i < len(searches); i++ {
		paramsArray = append(paramsArray, map[string]string{
			"search": url.QueryEscape(searches[i]),
		})
	}

	params := map[string]string{
		"limit":  strconv.Itoa(pageSize),
		"offset": strconv.Itoa(offset),
	}
	paramsArray = append(paramsArray, params)
	Url := fmt.Sprintf(UserNodeAssetsListURL, userID, nodeID)
	var err error
	if pageSize > 0 {
		_, err = authClient.Get(Url, &resp, paramsArray...)
	} else {
		var data model.AssetList
		_, err = authClient.Get(Url, &data, paramsArray...)
		resp.Data = data
		resp.Total = len(data)
	}
	if err != nil {
		logger.Error("Get user node assets error: ", err)
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

func ValidateUserDatabasePermission(userID, databaseID, systemUserID string) bool {
	payload := map[string]string{
		"user_id":         userID,
		"database_app_id": databaseID,
		"system_user_id":  systemUserID,
		"cache_policy":    "1",
	}
	Url := ValidateUserDatabasePermissionURL
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

func GetUserNodeTreeWithAsset(userID, nodeID, cachePolicy string) (nodeTrees model.NodeTreeList) {
	if cachePolicy == "" {
		cachePolicy = "1"
	}

	payload := map[string]string{"cache_policy": cachePolicy}
	if nodeID != "" {
		payload["id"] = nodeID
	}
	Url := fmt.Sprintf(NodeTreeWithAssetURL, userID)
	_, err := authClient.Get(Url, &nodeTrees, payload)
	if err != nil {
		logger.Error("Get user node tree error: ", err)
	}
	return
}

func SearchPermAsset(uid, key string) (res model.NodeTreeList, err error) {
	Url := fmt.Sprintf(UserAssetsTreeURL, uid)
	payload := map[string]string{"search": key}
	_, err = authClient.Get(Url, &res, payload)
	if err != nil {
		logger.Error("Get user node tree error: ", err)
	}
	return
}