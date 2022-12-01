package service

import (
	"strconv"
	"strings"

	"github.com/jumpserver/koko/pkg/jms-sdk-go/model"
)

func (s *JMService) getPaginationAssets(reqUrl string, param model.PaginationParam) (resp model.PaginationResponse, err error) {
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
	if param.Order != "" {
		params["order"] = param.Order
	}
	if param.Type != "" {
		params["type"] = param.Type
	}
	if param.Category != "" {
		params["category"] = param.Category
	}

	if param.IsActive {
		params["is_active"] = "true"
	}

	paramsArray = append(paramsArray, params)
	if param.PageSize > 0 {
		_, err = s.authClient.Get(reqUrl, &resp, paramsArray...)
	} else {
		var data []model.Asset
		_, err = s.authClient.Get(reqUrl, &data, paramsArray...)
		resp.Data = data
		resp.Total = len(data)
	}
	return
}
