package service

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/jumpserver/koko/pkg/jms-sdk-go/model"
)

func (s *JMService) GetAllUserPermMySQLs(userId string) ([]map[string]interface{}, error) {
	var param model.PaginationParam
	res, err := s.GetUserPermsDatabase(userId, param)
	if err != nil {
		return nil, err
	}
	return res.Data, err
}

func (s *JMService) GetAllUserPermK8s(userId string) ([]map[string]interface{}, error) {
	var param model.PaginationParam
	res, err := s.GetUserPermsK8s(userId, param)
	if err != nil {
		return nil, err
	}
	return res.Data, err
}

func (s *JMService) GetUserPermsMySQL(userId string, param model.PaginationParam) (resp model.PaginationResponse, err error) {
	reqUrl := fmt.Sprintf(UserPermsApplicationsURL, userId, model.AppTypeMySQL)
	return s.getPaginationResult(reqUrl, param)
}

func (s *JMService) GetUserPermsDatabase(userId string, param model.PaginationParam) (resp model.PaginationResponse, err error) {
	reqUrl := fmt.Sprintf(UserPermsDatabaseURL, userId)
	return s.getPaginationResult(reqUrl, param)
}

func (s *JMService) GetUserPermsK8s(userId string, param model.PaginationParam) (resp model.PaginationResponse, err error) {
	reqUrl := fmt.Sprintf(UserPermsApplicationsURL, userId, model.AppTypeK8s)
	return s.getPaginationResult(reqUrl, param)
}

func (s *JMService) getPaginationResult(reqUrl string, param model.PaginationParam) (resp model.PaginationResponse, err error) {
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
	if param.PageSize > 0 {
		_, err = s.authClient.Get(reqUrl, &resp, paramsArray...)
	} else {
		var data []map[string]interface{}
		_, err = s.authClient.Get(reqUrl, &data, paramsArray...)
		resp.Data = data
		resp.Total = len(data)
	}
	return
}
