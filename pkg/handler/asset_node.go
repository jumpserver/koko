package handler

import (
	"strings"

	"github.com/jumpserver-dev/sdk-go/model"
	"github.com/jumpserver/koko/pkg/i18n"
)

func (u *UserSelectHandler) retrieveRemoteNodeAsset(reqParam model.PaginationParam) []model.PermAsset {
	return u.retrieveRemoteAssetPage(reqParam, u.assetListPath(), nil)
}

func (u *UserSelectHandler) displayNodeAssetResult(searchHeader string) {
	lang := i18n.NewLang(u.h.i18nLang)
	if len(u.currentResult) == 0 {
		u.displayNoResultMsg(searchHeader, lang.T("No assets in this node"))
		return
	}
	u.displayAssets(searchHeader)
}

func nodeDisplayPath(nodes model.NodeList, selected model.Node) string {
	byKey := make(map[string]model.Node, len(nodes))
	for _, node := range nodes {
		byKey[node.Key] = node
	}
	keys := strings.Split(selected.Key, ":")
	names := make([]string, 0, len(keys))
	selectedFound := false
	for i := range keys {
		key := strings.Join(keys[:i+1], ":")
		if node, ok := byKey[key]; ok && strings.TrimSpace(node.Name) != "" {
			names = append(names, strings.TrimSpace(node.Name))
			selectedFound = selectedFound || key == selected.Key
		}
	}
	if !selectedFound {
		if name := strings.TrimSpace(selected.Name); name != "" {
			names = append(names, name)
		}
	}
	return "/" + strings.Join(names, "/")
}
