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
相同父节，按照 Name 排序
*/
func keyAndNameSort(node1, node2 *Node) bool {
	node1Keys := strings.Split(node1.Key, ":")
	node2Keys := strings.Split(node2.Key, ":")
	if len(node1Keys) == len(node2Keys) {
		switch len(node1Keys) {
		case 1:
			node1num, _ := strconv.Atoi(node1Keys[0])
			node2num, _ := strconv.Atoi(node2Keys[0])
			return node1num < node2num
		default:
			prefix := strings.Join(node2Keys[:len(node2Keys)-1], ":")
			if strings.HasPrefix(node1.Key, prefix) {
				return node1.Name < node2.Name
			}
		}
	}

	for i := 0; i < len(node1Keys); i++ {
		if i >= len(node2Keys) {
			return false
		}
		if node1Keys[i] == node2Keys[i] {
			continue
		}
		node1num, _ := strconv.Atoi(node1Keys[i])
		node2num, _ := strconv.Atoi(node2Keys[i])
		switch {
		case node1num > node2num:
			return false
		default:
			return true
		}
	}
	return true
}

func SortNodesByKeyAndName(nodes []Node) {
	nodeSortBy(keyAndNameSort).Sort(nodes)
}
