package handler

import (
	"fmt"
	"strings"

	"github.com/xlab/treeprint"
)

type classicTypeNode struct {
	ID           string
	Parent       string
	Name         string
	Kind         string
	Category     string
	AssetType    string
	AssetsAmount int
	Path         string
}

func (d tuiData) typeNodes() ([]classicTypeNode, error) {
	var page tuiTreePage
	client := newLangAPIClient(d.api, d.lang)
	_, err := client.Call("GET", d.userPath("nodes/children-with-assets/category/tree/"), nil, &page,
		map[string]string{"include_assets": "false"})
	if err != nil {
		return nil, err
	}
	nodes := make([]classicTypeNode, 0, len(page.Results))
	for _, item := range page.Results {
		kind := strings.TrimSpace(item.Meta.Type)
		if item.ID == "ROOT" || kind == "" || kind == "asset" {
			continue
		}
		if kind != "category" && kind != "type" {
			continue
		}
		name, amount := typeTreeAmount(item.Name)
		node := classicTypeNode{
			ID: item.ID, Parent: item.Parent, Name: strings.TrimSpace(name), Kind: kind,
			Category: item.Meta.Category, AssetsAmount: 0,
		}
		if amount != nil {
			node.AssetsAmount = *amount
		}
		switch kind {
		case "category":
			if node.Category == "" {
				node.Category = item.Meta.AssetType
			}
		case "type":
			node.AssetType = item.Meta.AssetType
		}
		if node.ID == "" || node.Name == "" {
			return nil, fmt.Errorf("type tree response is missing id or name")
		}
		if node.Category == "" || kind == "type" && node.AssetType == "" {
			return nil, fmt.Errorf("type tree response is missing category or type")
		}
		nodes = append(nodes, node)
	}
	return nodes, nil
}

func constructTypeTreeRows(nodes []classicTypeNode) ([]string, []classicTypeNode) {
	byID := make(map[string]struct{}, len(nodes))
	for _, node := range nodes {
		byID[node.ID] = struct{}{}
	}
	children := make(map[string][]int, len(nodes))
	roots := make([]int, 0)
	for i, node := range nodes {
		if node.Parent == "" || node.Parent == "ROOT" {
			roots = append(roots, i)
			continue
		}
		if _, ok := byID[node.Parent]; !ok {
			roots = append(roots, i)
			continue
		}
		children[node.Parent] = append(children[node.Parent], i)
	}
	prefixes := make([]string, 0, len(nodes))
	ordered := make([]classicTypeNode, 0, len(nodes))
	visited := make(map[string]struct{}, len(nodes))
	var walk func([]int, string, string)
	walk = func(indexes []int, prefix, parentPath string) {
		for position, index := range indexes {
			node := nodes[index]
			if _, ok := visited[node.ID]; ok {
				continue
			}
			visited[node.ID] = struct{}{}
			last := position == len(indexes)-1
			edge := string(treeprint.EdgeTypeMid)
			childPrefix := prefix + string(treeprint.EdgeTypeLink) + strings.Repeat(" ", treeprint.IndentSize)
			if last {
				edge = string(treeprint.EdgeTypeEnd)
				childPrefix = prefix + strings.Repeat(" ", treeprint.IndentSize+1)
			}
			node.Path = "/" + node.Name
			if parentPath != "" {
				node.Path = parentPath + "/" + node.Name
			}
			prefixes = append(prefixes, prefix+edge+" ")
			ordered = append(ordered, node)
			walk(children[node.ID], childPrefix, node.Path)
		}
	}
	walk(roots, "", "")
	for i := range nodes {
		if _, ok := visited[nodes[i].ID]; !ok {
			walk([]int{i}, "", "")
		}
	}
	return prefixes, ordered
}
