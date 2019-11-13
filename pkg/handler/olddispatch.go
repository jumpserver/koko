package handler

import (
	"context"
	"io"
	"strconv"
	"strings"

	"github.com/jumpserver/koko/pkg/config"
	"github.com/jumpserver/koko/pkg/logger"
	"github.com/jumpserver/koko/pkg/model"
	"github.com/jumpserver/koko/pkg/service"
)

func (h *interactiveHandler) Dispatch(ctx context.Context) {
	defer logger.Infof("Request %s: User %s stop interactive", h.sess.ID(), h.user.Name)
	for {
		line, err := h.term.ReadLine()
		if err != nil {
			logger.Debugf("User %s close connect", h.user.Name)
			break
		}
		line = strings.TrimSpace(line)
		switch len(line) {
		case 0, 1:
			switch strings.ToLower(line) {
			case "", "p":
				// 展示所有的资产
				h.displayAllAssets()
			case "g":
				<-h.firstLoadDone
				h.displayNodes(h.nodes)
			case "h":
				h.displayBanner()
			case "r":
				h.refreshAssetsAndNodesData()
			case "q":
				logger.Debugf("user %s enter to exit", h.user.Name)
				return
			default:
				h.searchAssetOrProxy(line)
			}
		default:
			switch {
			case line == "exit", line == "quit":
				logger.Debugf("user %s enter to exit", h.user.Name)
				return
			case strings.Index(line, "/") == 0:
				searchWord := strings.TrimSpace(line[1:])
				h.searchAsset(searchWord)
			case strings.Index(line, "g") == 0:
				searchWord := strings.TrimSpace(strings.TrimPrefix(line, "g"))
				if num, err := strconv.Atoi(searchWord); err == nil {
					if num >= 0 {
						assets := h.searchNodeAssets(num)
						h.displayAssets(assets)
						continue
					}
				}
				h.searchAssetOrProxy(line)
			default:
				h.searchAssetOrProxy(line)
			}
		}

	}
}

func (h *interactiveHandler) displayAllAssets() {
	switch h.assetLoadPolicy {
	case "all":
		<-h.firstLoadDone
		h.displayAssets(h.allAssets)
	default:
		pag := NewUserPagination(h.term, h.user.ID, "", false)
		result := pag.Start()
		if pag.IsNeedProxy && len(result) == 1 {
			h.searchResult = h.searchResult[:0]
			h.ProxyAsset(result[0])
		} else {
			h.searchResult = result
		}
	}
}

func (h *interactiveHandler) displayAssets(assets model.AssetList) {
	if len(assets) == 0 {
		_, _ = io.WriteString(h.term, getI18nFromMap("NoAssets")+"\n\r")
	} else {
		sortedAssets := assets.SortBy(config.GetConf().AssetListSortBy)
		pag := NewAssetPagination(h.term, sortedAssets)
		selectOneAssets := pag.Start()
		if len(selectOneAssets) == 1 {
			systemUsers := service.GetUserAssetSystemUsers(h.user.ID, selectOneAssets[0].ID)
			systemUser, ok := h.chooseSystemUser(selectOneAssets[0], systemUsers)
			if !ok {
				return
			}
			h.assetSelect = &selectOneAssets[0]
			h.systemUserSelect = &systemUser
			h.Proxy(context.TODO())
		}
		if pag.page.PageSize() >= pag.page.TotalCount() {
			h.searchResult = sortedAssets
		}
	}
}

func (h *interactiveHandler) displayNodes(nodes []model.Node) {
	tree := ConstructAssetNodeTree(nodes)
	_, _ = io.WriteString(h.term, "\n\r"+getI18nFromMap("NodeHeaderTip"))
	_, _ = io.WriteString(h.term, tree.String())
	_, err := io.WriteString(h.term, getI18nFromMap("NodeEndTip")+"\n\r")
	if err != nil {
		logger.Info("displayAssetNodes err:", err)
	}

}

func (h *interactiveHandler) searchAsset(key string) {
	switch h.assetLoadPolicy {
	case "all":
		<-h.firstLoadDone
		var searchData []model.Asset
		switch len(h.searchResult) {
		case 0:
			searchData = h.allAssets
		default:
			searchData = h.searchResult
		}
		assets := searchFromLocalAssets(searchData, key)
		h.displayAssets(assets)
	default:
		pag := NewUserPagination(h.term, h.user.ID, key, false)
		result := pag.Start()
		if pag.IsNeedProxy && len(result) == 1 {
			h.searchResult = h.searchResult[:0]
			h.ProxyAsset(result[0])
		} else {
			h.searchResult = result
		}
	}
}

func (h *interactiveHandler) searchAssetOrProxy(key string) {
	if indexNum, err := strconv.Atoi(key); err == nil && len(h.searchResult) > 0 {
		if indexNum > 0 && indexNum <= len(h.searchResult) {
			assetSelect := h.searchResult[indexNum-1]
			h.ProxyAsset(assetSelect)
			return
		}
	}
	var assets []model.Asset
	switch h.assetLoadPolicy {
	case "all":
		<-h.firstLoadDone
		var searchData []model.Asset
		switch len(h.searchResult) {
		case 0:
			searchData = h.allAssets
		default:
			searchData = h.searchResult
		}
		assets = searchFromLocalAssets(searchData, key)
		if len(assets) != 1 {
			h.displayAssets(assets)
			return
		}
	default:
		pag := NewUserPagination(h.term, h.user.ID, key, true)
		assets = pag.Start()
	}

	if len(assets) == 1 {
		h.ProxyAsset(assets[0])
	} else {
		h.searchResult = assets
	}
}

func (h *interactiveHandler) searchNodeAssets(num int) (assets model.AssetList) {
	if num > len(h.nodes) || num == 0 {
		return assets
	}
	node := h.nodes[num-1]
	assets = service.GetUserNodeAssets(h.user.ID, node.ID, "1")
	return
}
