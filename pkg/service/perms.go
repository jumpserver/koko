package service

import (
	"cocogo/pkg/model"
)

func GetUserAssets(userId string) (assets model.AssetList) {
	return model.AssetList{{Id: "xxxxxxxxx", Hostname: "test", Ip: "192.168.244.185", Port: 22}}
}

func GetUserNodes(userId string) (nodes model.NodeList) {
	return model.NodeList{{Id: "XXXXXXX", Name: "test"}}
}

func ValidateUserAssetPermission(userId, assetId, systemUserId string) bool {
	return true
}
