package handler

import (
	"fmt"
	"github.com/jumpserver/koko/pkg/logger"

	"github.com/jumpserver/koko/pkg/i18n"
	"github.com/jumpserver/koko/pkg/jms-sdk-go/model"
	"github.com/jumpserver/koko/pkg/utils"
)

func (u *UserSelectHandler) retrieveRemoteNodeAsset(reqParam model.PaginationParam) []model.Asset {
	res, err := u.h.jmsService.GetUserNodeAssets(u.user.ID, u.selectedNode.ID, reqParam)
	if err != nil {
		logger.Errorf("Get user %s node assets failed %s", u.user.Name, err)
	}
	return u.updateRemotePageData(reqParam, res)
}

func (u *UserSelectHandler) displayNodeAssetResult(searchHeader string) {
	term := u.h.term
	lang := i18n.NewLang(u.h.i18nLang)
	if len(u.currentResult) == 0 {
		noNodeAssets := fmt.Sprintf(lang.T("%s node has no assets"), u.selectedNode.Name)
		utils.IgnoreErrWriteString(term, utils.WrapperString(noNodeAssets, utils.Red))
		utils.IgnoreErrWriteString(term, utils.CharNewLine)
		utils.IgnoreErrWriteString(term, utils.WrapperString(searchHeader, utils.Green))
		utils.IgnoreErrWriteString(term, utils.CharNewLine)
		return
	}
	u.displayAssets(searchHeader)
}
