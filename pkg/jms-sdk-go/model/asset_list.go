package model

import (
	"sort"
	"strings"
)

type AssetList []Asset

func (a AssetList) SortBy(tp string) AssetList {
	var sortedAssets = make(AssetList, len(a))
	copy(sortedAssets, a)
	switch tp {
	case "ip":
		sorter := &assetSorter{
			data:   sortedAssets,
			sortBy: assetSortByIP,
		}
		sort.Sort(sorter)
	default:
		sorter := &assetSorter{
			data:   sortedAssets,
			sortBy: assetSortByHostName,
		}
		sort.Sort(sorter)
	}
	return sortedAssets
}

type assetSorter struct {
	data   []Asset
	sortBy func(asset1, asset2 *Asset) bool
}

func (s *assetSorter) Len() int {
	return len(s.data)
}

func (s *assetSorter) Swap(i, j int) {
	s.data[i], s.data[j] = s.data[j], s.data[i]
}

func (s *assetSorter) Less(i, j int) bool {
	return s.sortBy(&s.data[i], &s.data[j])
}

func assetSortByIP(asset1, asset2 *Asset) bool {
	iIPs := strings.Split(asset1.IP, ".")
	jIPs := strings.Split(asset2.IP, ".")
	for i := 0; i < len(iIPs); i++ {
		if i >= len(jIPs) {
			return false
		}
		if len(iIPs[i]) == len(jIPs[i]) {
			if iIPs[i] == jIPs[i] {
				continue
			} else {
				return iIPs[i] < jIPs[i]
			}
		} else {
			return len(iIPs[i]) < len(jIPs[i])
		}

	}
	return true

}

func assetSortByHostName(asset1, asset2 *Asset) bool {
	return asset1.Hostname < asset2.Hostname
}
