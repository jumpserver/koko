package service

import (
	"cocogo/pkg/sdk"
)

func GetUserAssets(userId string) (assets []sdk.Asset) {
	return
}

func GetUserNodes(userId string) (nodes []sdk.Node) {
	return
}

func ValidateUserAssetPermission(userId, assetId, systemUserId string) bool {
	return true
}
