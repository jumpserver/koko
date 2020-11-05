package handler

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/jumpserver/koko/pkg/common"
	"github.com/jumpserver/koko/pkg/config"
	"github.com/jumpserver/koko/pkg/i18n"
	"github.com/jumpserver/koko/pkg/logger"
	"github.com/jumpserver/koko/pkg/model"
	"github.com/jumpserver/koko/pkg/proxy"
	"github.com/jumpserver/koko/pkg/service"
	"github.com/jumpserver/koko/pkg/utils"
)


func (u *UserSelectHandler) retrieveRemoteAsset(reqParam model.PaginationParam) []map[string]interface{} {
	res := service.GetUserPermsAssets(u.user.ID, reqParam)
	return u.updateRemotePageData(reqParam, res)
}

func (u *UserSelectHandler) searchLocalAsset(searches ...string) []map[string]interface{} {
	/*
	   {
	       "id": "1ccad81f-76a6-4ee2-a3ac-e652ef3afecb",
	       "hostname": "127.0.0.1",
	       "ip": "192.168.1.97",
	       "protocols": [
	           "rdp/3389"
	       ],
	       "os": null,
	       "domain": null,
	       "platform": "Windows",
	       "comment": "",
	       "org_id": "",
	       "is_active": true,
	       "org_name": "DEFAULT"
	   },
	*/
	fields := map[string]struct{}{
		"name":     {},
		"hostname": {},
		"ip":       {},
		"comment":  {},
	}
	return u.searchLocalFromFields(fields, searches...)
}

func (u *UserSelectHandler) displayAssetResult(searchHeader string) {
	term := u.h.term

	if len(u.currentResult) == 0 {
		noAssets := i18n.T("No Assets")
		utils.IgnoreErrWriteString(term, utils.WrapperString(noAssets, utils.Red))
		utils.IgnoreErrWriteString(term, utils.CharNewLine)
		utils.IgnoreErrWriteString(term, utils.WrapperString(searchHeader, utils.Green))
		utils.IgnoreErrWriteString(term, utils.CharNewLine)
		return
	}
	u.displaySortedAssets(searchHeader)
}

func (u *UserSelectHandler) displaySortedAssets(searchHeader string) {
	switch config.GetConf().AssetListSortBy {
	case "ip":
		sortedAsset := IPAssetList(u.currentResult)
		sort.Sort(sortedAsset)
		u.currentResult = sortedAsset
	default:
		sortedAsset := HostnameAssetList(u.currentResult)
		sort.Sort(sortedAsset)
		u.currentResult = sortedAsset
	}
	term := u.h.term
	currentPage := u.CurrentPage()
	pageSize := u.PageSize()
	totalPage := u.TotalPage()
	totalCount := u.TotalCount()

	idLabel := i18n.T("ID")
	hostLabel := i18n.T("Hostname")
	ipLabel := i18n.T("IP")
	commentLabel := i18n.T("Comment")

	Labels := []string{idLabel, hostLabel, ipLabel, commentLabel}
	fields := []string{"ID", "Hostname", "IP", "Comment"}
	data := make([]map[string]string, len(u.currentResult))
	for i, j := range u.currentResult {
		row := make(map[string]string)
		row["ID"] = strconv.Itoa(i + 1)
		fieldMap := map[string]string{
			"hostname": "Hostname",
			"ip":       "IP",
			"comment":  "Comment",
		}
		row = convertMapItemToRow(j, fieldMap, row)
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
			"ID":       {0, 0, 5},
			"Hostname": {0, 8, 0},
			"IP":       {0, 15, 40},
			"Comment":  {0, 0, 0},
		},
		Data:        data,
		TotalSize:   w,
		Caption:     caption,
		TruncPolicy: common.TruncMiddle,
	}
	table.Initial()
	loginTip := i18n.T("Enter ID number directly login the asset, multiple search use // + field, such as: //16")
	pageActionTip := i18n.T("Page up: b	Page down: n")
	actionTip := fmt.Sprintf("%s %s", loginTip, pageActionTip)

	_, _ = term.Write([]byte(utils.CharClear))
	_, _ = term.Write([]byte(table.Display()))
	utils.IgnoreErrWriteString(term, utils.WrapperString(actionTip, utils.Green))
	utils.IgnoreErrWriteString(term, utils.CharNewLine)
	utils.IgnoreErrWriteString(term, utils.WrapperString(searchHeader, utils.Green))
	utils.IgnoreErrWriteString(term, utils.CharNewLine)
}

func (u *UserSelectHandler) proxyAsset(asset model.Asset) {
	systemUsers := service.GetUserAssetSystemUsers(u.user.ID, asset.ID)
	highestSystemUsers := selectHighestPrioritySystemUsers(systemUsers)
	selectedSystemUser, ok := u.h.chooseSystemUser(highestSystemUsers)
	if !ok {
		return
	}
	p := &proxy.ProxyServer{
		UserConn:   u.h.sess,
		User:       u.h.user,
		Asset:      &asset,
		SystemUser: &selectedSystemUser,
	}
	u.h.pauseWatchWinSize()
	p.Proxy()
	u.h.resumeWatchWinSize()
	logger.Infof("Request %s: asset %s proxy end", u.h.sess.Uuid, asset.Hostname)

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
