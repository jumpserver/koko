package handler

import (
	"fmt"

	"github.com/jumpserver/koko/pkg/i18n"
	"github.com/jumpserver/koko/pkg/model"
	"github.com/jumpserver/koko/pkg/service"
	"github.com/jumpserver/koko/pkg/utils"
)

func (u *UserSelectHandler) retrieveRemoteNodeAsset(reqParam model.PaginationParam) []map[string]interface{} {
	res := service.GetUserNodeAssets(u.user.ID, u.selectedNode.ID, reqParam)
	return u.updateRemotePageData(reqParam, res)
}

func (u *UserSelectHandler) displayNodeAssetResult(searchHeader string) {
	term := u.h.term
	if len(u.currentResult) == 0 {
		noNodeAssets := fmt.Sprintf(i18n.T("%s node has no assets"), u.selectedNode.Name)
		utils.IgnoreErrWriteString(term, utils.WrapperString(noNodeAssets, utils.Red))
		utils.IgnoreErrWriteString(term, utils.CharNewLine)
		utils.IgnoreErrWriteString(term, utils.WrapperString(searchHeader, utils.Green))
		utils.IgnoreErrWriteString(term, utils.CharNewLine)
		return
	}
	u.displaySortedAssets(searchHeader)
}
