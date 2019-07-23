package service

import (
	"fmt"
	"sync"

	"github.com/jumpserver/koko/pkg/logger"
	"github.com/jumpserver/koko/pkg/model"
)

var userAssetsCached = assetsCacheContainer{
	mapData: make(map[string]model.AssetList),
	mapETag: make(map[string]string),
	mu:      new(sync.RWMutex),
}

var userNodesCached = nodesCacheContainer{
	mapData: make(map[string]model.NodeList),
	mapETag: make(map[string]string),
	mu:      new(sync.RWMutex),
}

func GetUserAssetsFromCache(userID string) (assets model.AssetList, ok bool) {
	assets, ok = userAssetsCached.Get(userID)
	return
}

func GetUserAssets(userID, cachePolicy, assetId string) (assets model.AssetList) {
	if cachePolicy == "" {
		cachePolicy = "1"
	}
	headers := make(map[string]string)
	if etag, ok := userAssetsCached.GetETag(userID); ok && cachePolicy == "1" && assetId == "" {
		headers["If-None-Match"] = etag
	}
	payload := map[string]string{"cache_policy": cachePolicy}
	if assetId != "" {
		payload["id"] = assetId
	}
	Url := fmt.Sprintf(UserAssetsURL, userID)
	resp, err := authClient.Get(Url, &assets, payload, headers)

	if err != nil {
		logger.Error("Get user assets error: ", err)
		return
	}
	if resp.StatusCode == 200 && resp.Header.Get("ETag") != "" {
		newETag := resp.Header.Get("ETag")
		userAssetsCached.SetValue(userID, assets)
		userAssetsCached.SetETag(userID, newETag)
	} else if resp.StatusCode == 304 {
		assets, _ = userAssetsCached.Get(userID)
	}
	return
}

func GetUserNodesFromCache(userID string) (nodes model.NodeList, ok bool) {
	nodes, ok = userNodesCached.Get(userID)
	return
}

func GetUserNodes(userID, cachePolicy string) (nodes model.NodeList) {
	if cachePolicy == "" {
		cachePolicy = "1"
	}
	headers := make(map[string]string)
	if etag, ok := userNodesCached.GetETag(userID); ok && cachePolicy == "1" {
		headers["If-None-Match"] = etag
	}
	payload := map[string]string{"cache_policy": cachePolicy}
	Url := fmt.Sprintf(UserNodesListURL, userID)
	resp, err := authClient.Get(Url, &nodes, payload, headers)
	if err != nil {
		logger.Error("Get user nodes error: ", err)
	}
	if resp.StatusCode == 200 && resp.Header.Get("ETag") != "" {
		userNodesCached.SetValue(userID, nodes)
		userNodesCached.SetETag(userID, resp.Header.Get("ETag"))
	} else if resp.StatusCode == 304 {
		nodes, _ = userNodesCached.Get(userID)
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
