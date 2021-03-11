package service

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/jumpserver/koko/pkg/logger"
	"github.com/jumpserver/koko/pkg/model"
)

func GetUserPermsAssets(userID string, params model.PaginationParam) (resp model.PaginationResponse) {
	Url := fmt.Sprintf(UserPermsAssetsURL, userID)
	return getPaginationResult(Url, params)
}

func GetUserNodeAssets(userID, nodeID string, params model.PaginationParam) (resp model.PaginationResponse) {
	Url := fmt.Sprintf(UserPermsNodeAssetsListURL, userID, nodeID)
	return getPaginationResult(Url, params)
}

func GetAllUserPermsAssets(userId string) (assets []map[string]interface{}) {
	var params model.PaginationParam
	res := GetUserPermsAssets(userId, params)
	return res.Data
}

func RefreshUserAllPermsAssets(userId string) (assets []map[string]interface{}) {
	var params model.PaginationParam
	params.Refresh = true
	res := GetUserPermsAssets(userId, params)
	return res.Data
}

func getPaginationResult(reqUrl string, param model.PaginationParam) (resp model.PaginationResponse) {
	if param.PageSize < 0 {
		param.PageSize = 0
	}
	paramsArray := make([]map[string]string, 0, len(param.Searches)+2)
	for i := 0; i < len(param.Searches); i++ {
		paramsArray = append(paramsArray, map[string]string{
			"search": strings.TrimSpace(param.Searches[i]),
		})
	}

	params := map[string]string{
		"limit":  strconv.Itoa(param.PageSize),
		"offset": strconv.Itoa(param.Offset),
	}
	if param.Refresh {
		params["rebuild_tree"] = "1"
	}
	paramsArray = append(paramsArray, params)
	var err error
	if param.PageSize > 0 {
		_, err = authClient.Get(reqUrl, &resp, paramsArray...)
	} else {
		var data []map[string]interface{}
		_, err = authClient.Get(reqUrl, &data, paramsArray...)
		resp.Data = data
		resp.Total = len(data)
	}
	if err != nil {
		logger.Error("Get error: ", err)
	}
	return
}
