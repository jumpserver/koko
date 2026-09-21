package handler

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/jumpserver-dev/sdk-go/model"
	"github.com/jumpserver/koko/pkg/logger"
	"github.com/xlab/treeprint"
)

const classicTreeLimit = 5000

type classicFavoriteNode struct {
	ID           string
	Key          string
	Parent       string
	Name         string
	AssetsAmount int
	Path         string
}

func (d tuiData) favoriteNodes() ([]classicFavoriteNode, error) {
	client := newLangAPIClient(d.api, d.lang)
	path := d.userPath("favorite-tree/")
	fetchChildren := func(parentID string) ([]tuiTreeNode, error) {
		var page tuiTreePage
		_, err := client.Call("GET", path, nil, &page, map[string]string{
			"include_assets": "false",
			"parent_id":      parentID,
		})
		return page.Results, err
	}

	type frame struct {
		children []tuiTreeNode
		next     int
	}
	root, err := fetchChildren("")
	if err != nil {
		return nil, err
	}
	stack := []frame{{children: root}}
	nodes := make([]classicFavoriteNode, 0, len(root))
	seen := make(map[string]struct{})
	for len(stack) > 0 {
		current := &stack[len(stack)-1]
		if current.next >= len(current.children) {
			stack = stack[:len(stack)-1]
			continue
		}
		item := current.children[current.next]
		current.next++
		if item.Meta.Type != "node" {
			continue
		}
		id, key := item.Meta.Data.ID, item.Meta.Data.Key
		if key == "" {
			key = item.ID
		}
		if id == "" || key == "" {
			return nil, fmt.Errorf("favorite tree response is missing id or key")
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		name := strings.TrimSpace(item.Meta.Data.Value)
		if name == "" {
			name = strings.TrimSpace(item.Name)
		}
		if name == "" {
			return nil, fmt.Errorf("favorite tree response is missing name")
		}
		nodes = append(nodes, classicFavoriteNode{
			ID: id, Key: key, Parent: item.Parent, Name: name,
		})
		if len(nodes) > classicTreeLimit {
			return nil, fmt.Errorf("favorite tree exceeds %d folders", classicTreeLimit)
		}
		if id == "favorite-root" {
			continue
		}
		children, childErr := fetchChildren(id)
		if childErr != nil {
			return nil, childErr
		}
		if len(children) > 0 {
			stack = append(stack, frame{children: children})
		}
	}

	ids := make([]string, len(nodes))
	for i := range nodes {
		ids[i] = nodes[i].ID
	}
	for start := 0; start < len(ids); start += tuiTreeBatchSize {
		counts, countErr := d.nodeCounts("ROOT", 2, ids[start:min(start+tuiTreeBatchSize, len(ids))])
		if countErr != nil {
			return nil, countErr
		}
		for i := start; i < min(start+tuiTreeBatchSize, len(nodes)); i++ {
			nodes[i].AssetsAmount = counts[nodes[i].ID]
		}
	}
	return nodes, nil
}

func constructFavoriteTreeRows(nodes []classicFavoriteNode) ([]string, []classicFavoriteNode) {
	byKey := make(map[string]struct{}, len(nodes))
	for _, node := range nodes {
		byKey[node.Key] = struct{}{}
	}
	children := make(map[string][]int, len(nodes))
	roots := make([]int, 0, 1)
	for i, node := range nodes {
		if node.Parent == "" {
			roots = append(roots, i)
		} else if _, ok := byKey[node.Parent]; ok {
			children[node.Parent] = append(children[node.Parent], i)
		} else {
			roots = append(roots, i)
		}
	}
	prefixes := make([]string, 0, len(nodes))
	ordered := make([]classicFavoriteNode, 0, len(nodes))
	visited := make(map[string]struct{}, len(nodes))
	var walk func([]int, string, string)
	walk = func(indexes []int, prefix, parentPath string) {
		for position, index := range indexes {
			node := nodes[index]
			if _, ok := visited[node.Key]; ok {
				continue
			}
			visited[node.Key] = struct{}{}
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
			walk(children[node.Key], childPrefix, node.Path)
		}
	}
	walk(roots, "", "")
	return prefixes, ordered
}

func (u *UserSelectHandler) retrieveRemoteFavoriteAsset(reqParam model.PaginationParam) []model.PermAsset {
	pageSize := reqParam.PageSize
	loadAll := pageSize <= 0
	if loadAll {
		pageSize = 100
	}
	searches := make([]string, 0, len(reqParam.Searches))
	for _, search := range reqParam.Searches {
		searches = append(searches, strings.TrimSpace(search))
	}
	params := map[string]string{
		"limit":  strconv.Itoa(pageSize),
		"offset": strconv.Itoa(max(0, reqParam.Offset)),
		"order":  reqParam.Order,
	}
	if u.selectedFavorite.ID != "favorite-root" {
		params["folder_id"] = u.selectedFavorite.ID
	}
	if len(searches) > 0 {
		params["search"] = strings.Join(searches, ",")
	}
	var response model.PaginationResponse
	path := tuiData{userID: u.user.ID}.userPath("nodes/favorite/assets/")
	_, err := u.h.jmsService.Call("GET", path, nil, &response, params)
	if err != nil {
		logger.Errorf("Get user %s favorite assets failed: %s", u.user.Name, err)
	}
	if loadAll && err == nil {
		all := append([]model.PermAsset(nil), response.Data...)
		for response.NextURL != "" {
			response, err = u.h.jmsService.GetNextURLPermAssets(response.NextURL)
			if err != nil {
				logger.Errorf("Get user %s next favorite assets failed: %s", u.user.Name, err)
				break
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
