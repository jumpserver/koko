package service

import (
	"fmt"

	"cocogo/pkg/logger"
	"cocogo/pkg/model"
)

func GetUserAssets(userId, cachePolicy string) (assets model.AssetList) {
	if cachePolicy == "" {
		cachePolicy = "0"
	}
	payload := map[string]string{"cache_policy": cachePolicy}
	Url := fmt.Sprintf(UserAssetsURL, userId)
	err := authClient.Get(Url, &assets, payload)
	if err != nil {
		logger.Error("GetUserAssets---err")
	}
	return
}

func GetUserNodes(userId, cachePolicy string) (nodes model.NodeList) {
	if cachePolicy == "" {
		cachePolicy = "0"
	}
	payload := map[string]string{"cache_policy": cachePolicy}
	Url := fmt.Sprintf(UserNodesAssetsURL, userId)
	err := authClient.Get(Url, &nodes, payload)
	if err != nil {
		logger.Error("GetUserNodes err")
	}
	return
}

func ValidateUserAssetPermission(userId, assetId, systemUserId, action string) bool {
	payload := map[string]string{
		"user_id":        userId,
		"asset_id":       assetId,
		"system_user_id": systemUserId,
		"action_name":    action,
	}
	Url := ValidateUserAssetPermissionURL
	var res struct {
		Msg bool `json:"msg"`
	}
	err := authClient.Get(Url, &res, payload)

	if err != nil {
		logger.Error(err)
		return false
	}

	return res.Msg
}
