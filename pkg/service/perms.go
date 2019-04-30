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
	params := map[string]string{"cache_policy": cachePolicy}
	Url := authClient.ParseUrlQuery(fmt.Sprintf(UserAssetsURL, userId), params)
	err := authClient.Get(Url, &assets)
	if err != nil {
		logger.Error(err)
	}
	return
}

func GetUserNodes(userId, cachePolicy string) (nodes model.NodeList) {
	if cachePolicy == "" {
		cachePolicy = "0"
	}
	params := map[string]string{"cache_policy": cachePolicy}
	Url := authClient.ParseUrlQuery(fmt.Sprintf(UserNodesAssetsURL, userId), params)
	err := authClient.Get(Url, &nodes)
	if err != nil {
		logger.Error(err)
	}
	return
}

func ValidateUserAssetPermission(userId, assetId, systemUserId string) bool {
	params := map[string]string{
		"user_id":        userId,
		"asset_id":       assetId,
		"system_user_id": systemUserId,
		"cache_policy":   "1",
	}
	Url := authClient.ParseUrlQuery(ValidateUserAssetPermissionURL, params)
	err := authClient.Get(Url, nil)
	if err != nil {
		logger.Error(err)
		return false
	}

	return true
}
