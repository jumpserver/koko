package handler

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/jumpserver-dev/sdk-go/model"
	"github.com/xlab/treeprint"

	"github.com/jumpserver/koko/pkg/logger"
)

type classicTypeNode struct {
	ID           string
	Parent       string
	Name         string
	Kind         string
	Category     string
	AssetType    string
	PlatformID   string
	AssetsAmount int
	Path         string
}

func (u *UserSelectHandler) retrieveRemoteTypeAsset(reqParam model.PaginationParam) []model.PermAsset {
	if u.selectedType.Kind != "platform" || u.selectedType.PlatformID == "" {
		return u.retrieveRemoteAsset(reqParam)
	}
	pageSize := reqParam.PageSize
	loadAll := pageSize <= 0
	if loadAll {
		pageSize = 100
	}
	params := map[string]string{
		"limit":    strconv.Itoa(pageSize),
		"offset":   strconv.Itoa(max(0, reqParam.Offset)),
		"order":    reqParam.Order,
		"platform": u.selectedType.PlatformID,
	}
	if len(reqParam.Searches) > 0 {
		searches := make([]string, 0, len(reqParam.Searches))
		for _, search := range reqParam.Searches {
			searches = append(searches, strings.TrimSpace(search))
		}
		params["search"] = strings.Join(searches, ",")
	}
	var response model.PaginationResponse
	path := classicData{userID: u.user.ID}.userPath("assets/")
	_, err := u.h.jmsService.Call("GET", path, nil, &response, params)
	if err != nil {
		logger.Errorf("Get user %s platform assets failed: %s", u.user.Name, err)
		u.loadErr = err
		return nil
	}
	if loadAll {
		all := append([]model.PermAsset(nil), response.Data...)
		for response.NextURL != "" {
			response, err = u.h.jmsService.GetNextURLPermAssets(response.NextURL)
			if err != nil {
				logger.Errorf("Get user %s next platform assets failed: %s", u.user.Name, err)
				u.loadErr = err
				return nil
			}
			all = append(all, response.Data...)
		}
		response.Data = all
		response.Total = len(all)
		response.NextURL = ""
		response.PreviousURL = ""
	}
	assets := u.updateRemotePageData(reqParam, response)
	return u.prepareAssetPage(assets, path)
}

func (d classicData) typeNodes() ([]classicTypeNode, error) {
	var page classicTreePage
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
		name, amount := classicTreeAmount(item.Name)
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

// Lina's type tree takes category/type counts from the trailing label amount.
func classicTreeAmount(label string) (string, *int) {
	text := strings.TrimSpace(label)
	start := strings.LastIndex(text, "(")
	if start >= 0 && strings.HasSuffix(text, ")") {
		if count, err := strconv.Atoi(text[start+1 : len(text)-1]); err == nil && count >= 0 {
			return strings.TrimSpace(text[:start]), &count
		}
	}
	return label, nil
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
