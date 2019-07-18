package service

import (
	"fmt"

	"github.com/jumpserver/koko/pkg/logger"
	"github.com/jumpserver/koko/pkg/model"
)

func GetUserAssets(userID, cachePolicy, assetId string) (assets model.AssetList) {
	if cachePolicy == "" {
		cachePolicy = "1"
	}
	payload := map[string]string{"cache_policy": cachePolicy}
	if assetId != "" {
		payload["id"] = assetId
	}
	Url := fmt.Sprintf(UserAssetsURL, userID)
	err := authClient.Get(Url, &assets, payload)
	if err != nil {
		logger.Error("Get user assets error: ", err)
	}
	return
}

func GetUserNodes(userID, cachePolicy string) (nodes model.NodeList) {
	if cachePolicy == "" {
		cachePolicy = "1"
	}
	payload := map[string]string{"cache_policy": cachePolicy}
	Url := fmt.Sprintf(UserNodesAssetsURL, userID)
	err := authClient.Get(Url, &nodes, payload)
	if err != nil {
		logger.Error("Get user nodes error: ", err)
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
	err := authClient.Get(Url, &res, payload)

	if err != nil {
		logger.Error(err)
		return false
	}

	return res.Msg
}
