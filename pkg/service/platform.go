package service

import (
	"fmt"
	"github.com/jumpserver/koko/pkg/logger"
	"github.com/jumpserver/koko/pkg/model"
)

func GetAssetPlatform(assetId string) (platform model.Platform) {
	url := fmt.Sprintf(AssetPlatFormURL, assetId)
	if _, err := authClient.Get(url, &platform); err != nil{
		logger.Errorf("Get asset platform err: %s", err)
	}
	return
}
