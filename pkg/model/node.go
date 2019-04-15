package model

import (
	"sort"
	"strconv"
	"strings"
)

type AssetNode struct {
	Id            string  `json:"id"`
	Key           string  `json:"key"`
	Name          string  `json:"name"`
	Value         string  `json:"value"`
	Parent        string  `json:"parent"`
	AssetsGranted []Asset `json:"assets_granted"`
	AssetsAmount  int     `json:"assets_amount"`
	OrgId         string  `json:"org_id"`
}

type nodeSortBy func(node1, node2 *AssetNode) bool

func (by nodeSortBy) Sort(assetNodes []AssetNode) {
	nodeSorter := &AssetNodeSorter{
		assetNodes: assetNodes,
		sortBy:     by,
	}
	sort.Sort(nodeSorter)
}

type AssetNodeSorter struct {
	assetNodes []AssetNode
	sortBy     func(node1, node2 *AssetNode) bool
}

func (a *AssetNodeSorter) Len() int {
	return len(a.assetNodes)
}

func (a *AssetNodeSorter) Swap(i, j int) {
	a.assetNodes[i], a.assetNodes[j] = a.assetNodes[j], a.assetNodes[i]
}

func (a *AssetNodeSorter) Less(i, j int) bool {
	return a.sortBy(&a.assetNodes[i], &a.assetNodes[j])
}

/*
key的排列顺序：
1 1:3 1:3:0 1:4 1:5 1:8
*/
func keySort(node1, node2 *AssetNode) bool {
	node1Keys := strings.Split(node1.Key, ":")
	node2Keys := strings.Split(node2.Key, ":")
	for i := 0; i < len(node1Keys); i++ {
		if i >= len(node2Keys) {
			return false
		}
		node1num, _ := strconv.Atoi(node1Keys[i])
		node2num, _ := strconv.Atoi(node2Keys[i])
		if node1num == node2num {
			continue
		} else if node1num-node2num > 0 {
			return false
		} else {
			return true
		}

	}
	return true

}

func SortAssetNodesByKey(assetNodes []AssetNode) {
	nodeSortBy(keySort).Sort(assetNodes)
}
