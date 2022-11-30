package handler

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/jumpserver/koko/pkg/common"
	"github.com/jumpserver/koko/pkg/i18n"
	"github.com/jumpserver/koko/pkg/jms-sdk-go/model"
	"github.com/jumpserver/koko/pkg/logger"
	"github.com/jumpserver/koko/pkg/proxy"
	"github.com/jumpserver/koko/pkg/utils"
)

func (u *UserSelectHandler) retrieveRemoteAsset(reqParam model.PaginationParam) []model.Asset {
	res, err := u.h.jmsService.GetUserPermsAssets(u.user.ID, reqParam)
	if err != nil {
		logger.Errorf("Get user perm assets failed: %s", err.Error())
	}
	return u.updateRemotePageData(reqParam, res)
}

func (u *UserSelectHandler) searchLocalAsset(searches ...string) []model.Asset {

	fields := map[string]struct{}{
		"name":    {},
		"address": {},
		"ip":      {},
		//"platform": {},
		"org_name": {},
		"comment":  {},
	}
	return u.searchLocalFromFields(fields, searches...)
}

func (u *UserSelectHandler) displayAssetResult(searchHeader string) {
	term := u.h.term
	lang := i18n.NewLang(u.h.i18nLang)
	if len(u.currentResult) == 0 {
		noAssets := lang.T("No Assets")
		utils.IgnoreErrWriteString(term, utils.WrapperString(noAssets, utils.Red))
		utils.IgnoreErrWriteString(term, utils.CharNewLine)
		utils.IgnoreErrWriteString(term, utils.WrapperString(searchHeader, utils.Green))
		utils.IgnoreErrWriteString(term, utils.CharNewLine)
		return
	}
	u.displaySortedAssets(searchHeader)
}

func (u *UserSelectHandler) displaySortedAssets(searchHeader string) {
	lang := i18n.NewLang(u.h.i18nLang)
	term := u.h.term
	currentPage := u.CurrentPage()
	pageSize := u.PageSize()
	totalPage := u.TotalPage()
	totalCount := u.TotalCount()

	idLabel := lang.T("ID")
	hostLabel := lang.T("Hostname")
	ipLabel := lang.T("IP")
	platformLabel := lang.T("Platform")
	orgLabel := lang.T("Organization")
	commentLabel := lang.T("Comment")

	Labels := []string{idLabel, hostLabel, ipLabel, platformLabel, orgLabel, commentLabel}
	fields := []string{"ID", "Hostname", "IP", "Platform", "Organization", "Comment"}
	data := make([]map[string]string, len(u.currentResult))
	for i, j := range u.currentResult {
		row := make(map[string]string)
		row["ID"] = strconv.Itoa(i + 1)
		fieldMap := map[string]string{
			"name":     "Hostname",
			"address":  "IP",
			"platform": "Platform",
			"org_name": "Organization",
			"comment":  "Comment",
		}
		rowData := map[string]interface{}{
			"id":       j.ID,
			"name":     j.Name,
			"address":  j.Address,
			"platform": j.Platform.Name,
			"org_name": j.OrgName,
			"comment":  j.Comment,
		}

		row = convertMapItemToRow(rowData, fieldMap, row)
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
			"ID":           {0, 0, 5},
			"Hostname":     {0, 40, 0},
			"IP":           {0, 8, 40},
			"Platform":     {0, 8, 0},
			"Organization": {0, 8, 0},
			"Comment":      {0, 0, 0},
		},
		Data:        data,
		TotalSize:   w,
		Caption:     caption,
		TruncPolicy: common.TruncMiddle,
	}
	table.Initial()
	loginTip := lang.T("Enter ID number directly login the asset, multiple search use // + field, such as: //16")
	pageActionTip := lang.T("Page up: b	Page down: n")
	actionTip := fmt.Sprintf("%s %s", loginTip, pageActionTip)

	_, _ = term.Write([]byte(utils.CharClear))
	_, _ = term.Write([]byte(table.Display()))
	utils.IgnoreErrWriteString(term, utils.WrapperString(actionTip, utils.Green))
	utils.IgnoreErrWriteString(term, utils.CharNewLine)
	utils.IgnoreErrWriteString(term, utils.WrapperString(searchHeader, utils.Green))
	utils.IgnoreErrWriteString(term, utils.CharNewLine)
}

func (u *UserSelectHandler) proxyAsset(asset model.Asset) {
	systemUsers, err := u.h.jmsService.GetSystemUsersByUserIdAndAssetId(u.user.ID, asset.ID)
	if err != nil {
		return
	}
	highestSystemUsers := selectHighestPrioritySystemUsers(systemUsers)
	selectedSystemUser, ok := u.h.chooseSystemUser(highestSystemUsers)

	if !ok {
		return
	}
	i18nLang := u.h.i18nLang
	srv, err := proxy.NewServer(u.h.sess,
		u.h.jmsService,
		proxy.ConnectProtocol(selectedSystemUser.Protocol),
		proxy.ConnectI18nLang(i18nLang),
		proxy.ConnectUser(u.h.user),
		proxy.ConnectAsset(&asset),
		//proxy.ConnectSystemUser(&selectedSystemUser),
	)
	if err != nil {
		logger.Error(err)
		return
	}
	srv.Proxy()
	logger.Infof("Request %s: asset %s proxy end", u.h.sess.Uuid, asset.Name)

}

var (
	_ sort.Interface = (HostnameAssetList)(nil)
	_ sort.Interface = (IPAssetList)(nil)
)

type HostnameAssetList []map[string]interface{}

func (l HostnameAssetList) Len() int {
	return len(l)
}

func (l HostnameAssetList) Less(i, j int) bool {
	iHostnameValue := l[i]["hostname"]
	jHostnameValue := l[j]["hostname"]
	iHostname, ok := iHostnameValue.(string)
	if !ok {
		return false
	}
	jHostname, ok := jHostnameValue.(string)
	if !ok {
		return false
	}
	return CompareString(iHostname, jHostname)
}

func (l HostnameAssetList) Swap(i, j int) {
	l[j], l[i] = l[i], l[j]
}

type IPAssetList []map[string]interface{}

func (l IPAssetList) Len() int {
	return len(l)
}

func (l IPAssetList) Less(i, j int) bool {
	iIPValue := l[i]["ip"]
	jIPValue := l[j]["ip"]
	iIP, ok := iIPValue.(string)
	if !ok {
		return false
	}
	jIP, ok := jIPValue.(string)
	if !ok {
		return false
	}
	return CompareIP(iIP, jIP)
}

func (l IPAssetList) Swap(i, j int) {
	l[j], l[i] = l[i], l[j]
}

func CompareIP(ipA, ipB string) bool {
	iIPs := strings.Split(ipA, ".")
	jIPs := strings.Split(ipB, ".")
	for i := 0; i < len(iIPs); i++ {
		if i >= len(jIPs) {
			return false
		}
		if len(iIPs[i]) == len(jIPs[i]) {
			if iIPs[i] == jIPs[i] {
				continue
			} else {
				return iIPs[i] < jIPs[i]
			}
		} else {
			return len(iIPs[i]) < len(jIPs[i])
		}

	}
	return true
}

func CompareString(a, b string) bool {
	return a < b
}
