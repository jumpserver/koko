package handler

import (
	"fmt"
	"strconv"

	"github.com/jumpserver/koko/pkg/common"
	"github.com/jumpserver/koko/pkg/i18n"
	"github.com/jumpserver/koko/pkg/jms-sdk-go/model"
	"github.com/jumpserver/koko/pkg/logger"
	"github.com/jumpserver/koko/pkg/utils"
)

func (u *UserSelectHandler) retrieveRemoteK8s(reqParam model.PaginationParam) []map[string]interface{} {
	res, err := u.h.jmsService.GetUserPermsK8s(u.user.ID, reqParam)
	if err != nil {
		logger.Errorf("Get user perm k8s failed: %s", err.Error())
	}
	return u.updateRemotePageData(reqParam, res)
}

func (u *UserSelectHandler) searchLocalK8s(searches ...string) []map[string]interface{} {
	/*
		{
		            "id": "0a318338-65ca-4e33-80ec-daf11d6d6c9a",
		            "name": "kube",
		            "domain": null,
		            "category": "cloud",
		            "type": "k8s",
		            "attrs": {
		                "cluster": "https://127.0.0.1:8443"
		            },
		            "comment": "https://127.0.0.1:8443",
		            "org_id": "",
		            "category_display": "Cloud",
		            "type_display": "Kubernetes",
		            "org_name": "DEFAULT"
		        }
	*/
	//searchFields := []string{"name", "cluster", "comment"}

	fields := map[string]struct{}{
		"name":    {},
		"cluster": {},
		"comment": {},
	}
	return u.searchLocalFromFields(fields, searches...)
}

func (u *UserSelectHandler) displayK8sResult(searchHeader string) {
	currentDBS := u.currentResult
	term := u.h.term
	lang := i18n.NewLang(u.h.i18nLang)
	if len(currentDBS) == 0 {
		noK8s := lang.T("No kubernetes")
		utils.IgnoreErrWriteString(term, utils.WrapperString(noK8s, utils.Red))
		utils.IgnoreErrWriteString(term, utils.CharNewLine)
		utils.IgnoreErrWriteString(term, utils.WrapperString(searchHeader, utils.Green))
		utils.IgnoreErrWriteString(term, utils.CharNewLine)
		return
	}

	currentPage := u.CurrentPage()
	pageSize := u.PageSize()
	totalPage := u.TotalPage()
	totalCount := u.TotalCount()

	idLabel := lang.T("ID")
	nameLabel := lang.T("Name")
	clusterLabel := lang.T("Cluster")
	commentLabel := lang.T("Comment")

	Labels := []string{idLabel, nameLabel, clusterLabel, commentLabel}
	fields := []string{"ID", "Name", "Cluster", "Comment"}
	data := make([]map[string]string, len(currentDBS))
	for i, j := range currentDBS {
		row := make(map[string]string)
		row["ID"] = strconv.Itoa(i + 1)
		filedMap := map[string]string{
			"name":    "Name",
			"cluster": "Cluster",
			"comment": "Comment",
		}
		row = convertMapItemToRow(j, filedMap, row)
		row["Comment"] = joinMultiLineString(row["Comment"])
		data[i] = row
	}
	w, _ := term.GetSize()

	caption := fmt.Sprintf(lang.T("Page: %d, Count: %d, Total Page: %d, Total Count: %d"),
		currentPage, pageSize, totalPage, totalCount)

	caption = utils.WrapperString(caption, utils.Green)
	table := common.WrapperTable{
		Fields: fields,
		Labels: Labels,
		FieldsSize: map[string][3]int{
			"ID":      {0, 0, 5},
			"Name":    {0, 8, 0},
			"Cluster": {0, 20, 0},
			"Comment": {0, 0, 0},
		},
		Data:        data,
		TotalSize:   w,
		Caption:     caption,
		TruncPolicy: common.TruncMiddle,
	}
	table.Initial()

	loginTip := lang.T("Enter ID number directly login the kubernetes, multiple search use // + field, such as: //16")
	pageActionTip := lang.T("Page up: b	Page down: n")
	actionTip := fmt.Sprintf("%s %s", loginTip, pageActionTip)
	_, _ = term.Write([]byte(utils.CharClear))
	_, _ = term.Write([]byte(table.Display()))
	utils.IgnoreErrWriteString(term, utils.WrapperString(actionTip, utils.Green))
	utils.IgnoreErrWriteString(term, utils.CharNewLine)
	utils.IgnoreErrWriteString(term, utils.WrapperString(searchHeader, utils.Green))
	utils.IgnoreErrWriteString(term, utils.CharNewLine)
}
