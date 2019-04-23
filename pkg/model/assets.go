package model

import "cocogo/pkg/sdk"

type AssetList []sdk.Asset

func (a *AssetList) SortBy(tp string) AssetList {
	switch tp {
	case "ip":
		return []sdk.Asset{}
	default:
		return []sdk.Asset{}
	}
}

type NodeList []sdk.Node
