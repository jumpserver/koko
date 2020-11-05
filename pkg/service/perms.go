package service

import (
	"fmt"

	"github.com/jumpserver/koko/pkg/logger"
	"github.com/jumpserver/koko/pkg/model"
)

func GetUserAssetByID(userID, assertID string) (assets []model.Asset) {
	params := map[string]string{
		"id": assertID,
	}
	Url := fmt.Sprintf(UserPermsAssetsURL, userID)
	_, err := authClient.Get(Url, &assets, params)
	if err != nil {
		logger.Error("Get user asset by ID error: ", err)
	}
	return
}

func GetUserNodes(userId string) (nodes model.NodeList) {
	Url := fmt.Sprintf(UserPermsNodesListURL, userId)
	_, err := authClient.Get(Url, &nodes)
	if err != nil {
		logger.Error("Get user nodes error: ", err)
	}
	return
}

func GetUserAssetSystemUsers(userID, assetID string) (sysUsers []model.SystemUser) {
	Url := fmt.Sprintf(UserPermsAssetSystemUsersURL, userID, assetID)
	_, err := authClient.Get(Url, &sysUsers)
	if err != nil {
		logger.Error("Get user asset system users error: ", err)
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

func GetUserNodeTreeWithAsset(userID, nodeKey, cachePolicy string) (nodeTrees model.NodeTreeList) {
	if cachePolicy == "" {
		cachePolicy = "1"
	}

	payload := map[string]string{"cache_policy": cachePolicy}
	if nodeKey != "" {
		payload["key"] = nodeKey
	}
	Url := fmt.Sprintf(UserPermsNodeTreeWithAssetURL, userID)
	_, err := authClient.Get(Url, &nodeTrees, payload)
	if err != nil {
		logger.Error("Get user node tree error: ", err)
	}
	return
}

func SearchPermAsset(uid, key string) (res model.AssetList, err error) {
	Url := fmt.Sprintf(UserPermsAssetsURL, uid)
	payload := map[string]string{"search": key}
	_, err = authClient.Get(Url, &res, payload)
	if err != nil {
		logger.Errorf("Get user node tree error: %s", err)
	}
	return
}
