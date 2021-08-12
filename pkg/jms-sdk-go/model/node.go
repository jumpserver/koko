package model

import (
	"sort"
	"strconv"
	"strings"
)

type NodeList []Node

type Node struct {
	ID           string `json:"id"`
	Key          string `json:"key"`
	Name         string `json:"name"`
	Value        string `json:"value"`
	Parent       string `json:"parent"`
	AssetsAmount int    `json:"assets_amount"`
	OrgID        string `json:"org_id"`
}

type nodeSortBy func(node1, node2 *Node) bool

func (by nodeSortBy) Sort(nodes []Node) {
	nodeSorter := &AssetNodeSorter{
		nodes:  nodes,
		sortBy: by,
	}
	sort.Sort(nodeSorter)
}

type AssetNodeSorter struct {
	nodes  []Node
	sortBy func(node1, node2 *Node) bool
}

func (a *AssetNodeSorter) Len() int {
	return len(a.nodes)
}

func (a *AssetNodeSorter) Swap(i, j int) {
	a.nodes[i], a.nodes[j] = a.nodes[j], a.nodes[i]
}

func (a *AssetNodeSorter) Less(i, j int) bool {
	return a.sortBy(&a.nodes[i], &a.nodes[j])
}

/*
key的排列顺序：
1 1:3 1:3:0 1:4 1:5 1:8
*/
func keySort(node1, node2 *Node) bool {
	node1Keys := strings.Split(node1.Key, ":")
	node2Keys := strings.Split(node2.Key, ":")
	for i := 0; i < len(node1Keys); i++ {
		if i >= len(node2Keys) {
			return false
		}
		if node1Keys[i] == node2Keys[i] {
			continue
		}
		node1num, err := strconv.Atoi(node1Keys[i])
		if err != nil {
			return true
		}
		node2num, err := strconv.Atoi(node2Keys[i])
		if err != nil {
			return false
		}
		return node1num < node2num
	}
	return true
}

func SortNodesByKey(nodes []Node) {
	nodeSortBy(keySort).Sort(nodes)
}