package handler

import (
	"fmt"
	"strconv"

	"github.com/jumpserver/koko/pkg/common"
	"github.com/jumpserver/koko/pkg/i18n"
	"github.com/jumpserver/koko/pkg/logger"
	"github.com/jumpserver/koko/pkg/model"
	"github.com/jumpserver/koko/pkg/proxy"
	"github.com/jumpserver/koko/pkg/service"
	"github.com/jumpserver/koko/pkg/utils"
)

func (u *UserSelectHandler) retrieveRemoteK8s(reqParam model.PaginationParam) []map[string]interface{} {
	res := service.GetUserPermsK8s(u.user.ID, reqParam)
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
	if len(currentDBS) == 0 {
		noK8s := i18n.T("No kubernetes")
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

	idLabel := i18n.T("ID")
	nameLabel := i18n.T("Name")
	clusterLabel := i18n.T("Cluster")
	commentLabel := i18n.T("Comment")

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

	caption := fmt.Sprintf(i18n.T("Page: %d, Count: %d, Total Page: %d, Total Count: %d"),
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

	loginTip := i18n.T("Enter ID number directly login the kubernetes, multiple search use // + field, such as: //16")
	pageActionTip := i18n.T("Page up: b	Page down: n")
	actionTip := fmt.Sprintf("%s %s", loginTip, pageActionTip)
	_, _ = term.Write([]byte(utils.CharClear))
	_, _ = term.Write([]byte(table.Display()))
	utils.IgnoreErrWriteString(term, utils.WrapperString(actionTip, utils.Green))
	utils.IgnoreErrWriteString(term, utils.CharNewLine)
	utils.IgnoreErrWriteString(term, utils.WrapperString(searchHeader, utils.Green))
	utils.IgnoreErrWriteString(term, utils.CharNewLine)
}

func (u *UserSelectHandler) proxyK8s(k8sApp model.K8sApplication) {
	systemUsers := service.GetUserApplicationSystemUsers(u.user.ID, k8sApp.Id)
	highestSystemUsers := selectHighestPrioritySystemUsers(systemUsers)
	selectedSystemUser, ok := u.h.chooseSystemUser(highestSystemUsers)
	if !ok {
		return
	}

	p := proxy.K8sProxyServer{
		UserConn:   u.h.sess,
		User:       u.h.user,
		Cluster:    &k8sApp,
		SystemUser: &selectedSystemUser,
	}
	u.h.pauseWatchWinSize()
	p.Proxy()
	u.h.resumeWatchWinSize()
	logger.Infof("Request %s: k8s %s proxy end", u.h.sess.Uuid, k8sApp.Name)
}
