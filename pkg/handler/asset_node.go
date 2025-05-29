package handler

import (
	"fmt"

	"github.com/jumpserver-dev/sdk-go/model"
	"github.com/jumpserver/koko/pkg/i18n"
	"github.com/jumpserver/koko/pkg/logger"
)

func (u *UserSelectHandler) retrieveRemoteNodeAsset(reqParam model.PaginationParam) []model.PermAsset {
	res, err := u.h.jmsService.GetUserNodeAssets(u.user.ID, u.selectedNode.ID, reqParam)
	if err != nil {
		logger.Errorf("Get user %s node assets failed %s", u.user.Name, err)
	}
	return u.updateRemotePageData(reqParam, res)
}

func (u *UserSelectHandler) displayNodeAssetResult(searchHeader string) {
	lang := i18n.NewLang(u.h.i18nLang)
	if len(u.currentResult) == 0 {
		noNodeAssets := fmt.Sprintf(lang.T("%s node has no assets"), u.selectedNode.Name)
		u.displayNoResultMsg(searchHeader, noNodeAssets)
		return
	}
	u.displayAssets(searchHeader)
}
