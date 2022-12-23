package handler

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/jumpserver/koko/pkg/i18n"
	"github.com/jumpserver/koko/pkg/jms-sdk-go/model"
	"github.com/jumpserver/koko/pkg/logger"
	"github.com/jumpserver/koko/pkg/utils"
)

type dataSource string

const (
	loadingFromLocal  dataSource = "local"
	loadingFromRemote dataSource = "remote"
)

type selectType int

const (
	TypeAsset selectType = iota + 1
	TypeNodeAsset
	TypeK8s
	TypeDatabase
)

type UserSelectHandler struct {
	user *model.User
	h    *InteractiveHandler

	loadingPolicy dataSource
	currentType   selectType
	searchKeys    []string

	hasPre  bool
	hasNext bool

	allLocalData []model.Asset

	selectedNode  model.Node
	currentResult []model.Asset

	*pageInfo
}

func (u *UserSelectHandler) SetSelectType(s selectType) {
	u.SetLoadPolicy(loadingFromRemote) // default remote
	switch s {
	case TypeAsset:
		switch u.h.assetLoadPolicy {
		case "all":
			u.SetLoadPolicy(loadingFromLocal)
			u.AutoCompletion()
		}
		u.h.term.SetPrompt("[Host]> ")
	case TypeNodeAsset:
		u.h.term.SetPrompt("[Host]> ")
	case TypeK8s:
		u.h.term.SetPrompt("[K8S]> ")
	case TypeDatabase:
		u.h.term.SetPrompt("[DB]> ")
	}
	u.currentType = s
}

func (u *UserSelectHandler) AutoCompletion() {
	assets := u.Retrieve(0, 0, "")
	suggests := make([]string, 0, len(assets))

	for _, v := range assets {
		suggests = append(suggests, v.Name)
	}

	sort.Strings(suggests)
	u.h.term.AutoCompleteCallback = func(line string, pos int, key rune) (newLine string, newPos int, ok bool) {
		if key == 9 {
			termWidth, _ := u.h.GetPtySize()
			if len(line) >= 1 {
				sugs := utils.FilterPrefix(suggests, line)
				if len(sugs) >= 1 {
					commonPrefix := utils.LongestCommonPrefix(sugs)
					switch u.currentType {
					case TypeAsset, TypeNodeAsset:
						fmt.Fprintf(u.h.term, "%s%s\n%s\n", "[Host]> ", line, utils.Pretty(sugs, termWidth))
					case TypeK8s:
						fmt.Fprintf(u.h.term, "%s%s\n%s\n", "[K8S]> ", line, utils.Pretty(sugs, termWidth))
					case TypeDatabase:
						fmt.Fprintf(u.h.term, "%s%s\n%s\n", "[DB]> ", line, utils.Pretty(sugs, termWidth))
					}
					return commonPrefix, len(commonPrefix), true
				}
			}
		}

		return newLine, newPos, false
	}
}

func (u *UserSelectHandler) SetNode(node model.Node) {
	u.SetSelectType(TypeNodeAsset)
	u.selectedNode = node
}

func (u *UserSelectHandler) SetAllLocalData(data []model.Asset) {
	// 使用副本
	u.allLocalData = make([]model.Asset, len(data))
	copy(u.allLocalData, data)
}

func (u *UserSelectHandler) SetLoadPolicy(policy dataSource) {
	u.loadingPolicy = policy
}

func (u *UserSelectHandler) MoveNextPage() {
	if u.HasNext() {
		offset := u.CurrentOffSet()
		newPageSize := getPageSize(u.h, u.h.terminalConf)
		u.currentResult = u.Retrieve(newPageSize, offset, u.searchKeys...)
	}
	u.DisplayCurrentResult()
}

func (u *UserSelectHandler) MovePrePage() {
	if u.HasPrev() {
		offset := u.CurrentOffSet()
		newPageSize := getPageSize(u.h, u.h.terminalConf)
		start := offset - newPageSize*2
		if start <= 0 {
			start = 0
		}
		u.currentResult = u.Retrieve(newPageSize, start, u.searchKeys...)
	}
	u.DisplayCurrentResult()
}

func (u *UserSelectHandler) Search(key string) {
	newPageSize := getPageSize(u.h, u.h.terminalConf)
	u.currentResult = u.Retrieve(newPageSize, 0, key)
	u.searchKeys = []string{key}
	u.DisplayCurrentResult()
}

func (u *UserSelectHandler) SearchAgain(key string) {
	u.searchKeys = append(u.searchKeys, key)
	newPageSize := getPageSize(u.h, u.h.terminalConf)
	u.currentResult = u.Retrieve(newPageSize, 0, u.searchKeys...)
	u.DisplayCurrentResult()
}

func (u *UserSelectHandler) SearchOrProxy(key string) {
	if indexNum, err := strconv.Atoi(key); err == nil && len(u.currentResult) > 0 {
		if indexNum > 0 && indexNum <= len(u.currentResult) {
			u.Proxy(u.currentResult[indexNum-1])
			return
		}
	}

	newPageSize := getPageSize(u.h, u.h.terminalConf)
	currentResult := u.Retrieve(newPageSize, 0, key)
	u.currentResult = currentResult
	u.searchKeys = []string{key}
	if len(currentResult) == 1 {
		u.Proxy(currentResult[0])
		return
	}

	// 资产类型, 返回结果 ip 或者 hostname 与 key 完全一样则直接登录
	switch u.currentType {
	case TypeAsset:
		if strings.TrimSpace(key) != "" {
			if ret, ok := getUniqueAssetFromKey(key, currentResult); ok {
				u.Proxy(ret)
				return
			}
		}
	}
	u.DisplayCurrentResult()
}

func (u *UserSelectHandler) HasPrev() bool {
	return u.hasPre
}

func (u *UserSelectHandler) HasNext() bool {
	return u.hasNext
}

func (u *UserSelectHandler) DisplayCurrentResult() {
	lang := i18n.NewLang(u.h.i18nLang)
	searchHeader := fmt.Sprintf(lang.T("Search: %s"), strings.Join(u.searchKeys, " "))
	switch u.currentType {
	case TypeDatabase:
		u.displayDatabaseResult(searchHeader)
	case TypeK8s:
		u.displayK8sResult(searchHeader)
	case TypeNodeAsset:
		u.displayNodeAssetResult(searchHeader)
	case TypeAsset:
		u.displayAssetResult(searchHeader)
	default:
		logger.Error("Display unknown type")
	}
}

func (u *UserSelectHandler) Proxy(target model.Asset) {
	u.proxyAsset(target)
}

func (u *UserSelectHandler) Retrieve(pageSize, offset int, searches ...string) []model.Asset {
	switch u.loadingPolicy {
	case loadingFromLocal:
		return u.retrieveFromLocal(pageSize, offset, searches...)
	default:
		return u.retrieveFromRemote(pageSize, offset, searches...)
	}
}

func (u *UserSelectHandler) retrieveFromLocal(pageSize, offset int, searches ...string) []model.Asset {
	if pageSize <= 0 {
		pageSize = PAGESIZEALL
	}
	if offset < 0 {
		offset = 0
	}

	searchResult := u.retrieveLocal(searches...)
	var (
		totalData       []model.Asset
		total           int
		currentOffset   int
		currentPageSize int
	)

	if offset < len(searchResult) {
		totalData = searchResult[offset:]
	}
	total = len(totalData)
	currentPageSize = pageSize
	currentData := totalData

	if currentPageSize < 0 || currentPageSize == PAGESIZEALL {
		currentPageSize = len(totalData)
	}
	if total > currentPageSize {
		currentData = totalData[:currentPageSize]
	}
	currentOffset = offset + len(currentData)
	u.updatePageInfo(currentPageSize, total, currentOffset)
	u.hasPre = false
	u.hasNext = false
	if u.currentPage > 1 {
		u.hasPre = true
	}
	if u.currentPage < u.totalPage {
		u.hasNext = true
	}
	return currentData
}

func (u *UserSelectHandler) retrieveLocal(searches ...string) []model.Asset {
	switch u.currentType {
	case TypeDatabase:
		return u.searchLocalDatabase(searches...)
	case TypeK8s:
		return u.searchLocalK8s(searches...)
	case TypeAsset:
		return u.searchLocalAsset(searches...)
	default:
		// TypeAsset
		u.SetSelectType(TypeAsset)
		logger.Info("Retrieve default local data type: Asset")
		return u.searchLocalAsset(searches...)
	}
}

func (u *UserSelectHandler) searchLocalFromFields(fields map[string]struct{}, searches ...string) []model.Asset {
	items := make([]model.Asset, 0, len(u.allLocalData))
	for i := range u.allLocalData {
		assetData := u.allLocalData[i]
		data := map[string]interface{}{
			"name":     u.allLocalData[i].Name,
			"address":  assetData.Address,
			"db_name":  assetData.Specific.DBName,
			"org_name": assetData.OrgName,
			"platform": assetData.Platform.Name,
			"comment":  assetData.Comment,
		}
		if containKeysInMapItemFields(data, fields, searches...) {
			items = append(items, u.allLocalData[i])
		}
	}
	return items
}

func (u *UserSelectHandler) retrieveFromRemote(pageSize, offset int, searches ...string) []model.Asset {

	var order string
	switch u.h.terminalConf.AssetListSortBy {
	case "ip":
		order = "address"
	default:
		order = "name"
	}
	reqParam := model.PaginationParam{
		PageSize: pageSize,
		Offset:   offset,
		Searches: searches,
		Order:    order,
		IsActive: true,
	}
	switch u.currentType {
	case TypeDatabase:
		reqParam.Category = "database"
		return u.retrieveRemoteAsset(reqParam)
	case TypeK8s:
		reqParam.Type = "k8s"
		return u.retrieveRemoteAsset(reqParam)
	case TypeNodeAsset:
		return u.retrieveRemoteNodeAsset(reqParam)
	case TypeAsset:
		reqParam.Category = "host"
		return u.retrieveRemoteAsset(reqParam)
	default:
		reqParam.Category = "host"
		// TypeAsset
		u.SetSelectType(TypeAsset)
		logger.Info("Retrieve default remote data type: Asset")
		return u.retrieveRemoteAsset(reqParam)
	}
}

func (u *UserSelectHandler) updateRemotePageData(reqParam model.PaginationParam,
	res model.PaginationResponse) []model.Asset {
	u.hasNext = false
	u.hasPre = false

	if res.NextURL != "" {
		u.hasNext = true
	}
	if res.PreviousURL != "" {
		u.hasPre = true
	}
	total := res.Total
	currentPageSize := reqParam.PageSize
	currentData := res.Data
	if currentPageSize < 0 || currentPageSize == PAGESIZEALL {
		currentPageSize = len(res.Data)
	}
	if len(res.Data) > currentPageSize {
		currentData = currentData[:currentPageSize]
	}
	currentOffset := reqParam.Offset + len(currentData)
	u.updatePageInfo(currentPageSize, total, currentOffset)
	return currentData
}

func containKeysInMapItemFields(item map[string]interface{},
	searchFields map[string]struct{}, matchedKeys ...string) bool {

	if len(matchedKeys) == 0 {
		return true
	}
	if len(matchedKeys) == 1 && matchedKeys[0] == "" {
		return true
	}

	for key, value := range item {
		if _, ok := searchFields[key]; ok {
			switch result := value.(type) {
			case string:
				for i := range matchedKeys {
					if strings.Contains(result, matchedKeys[i]) {
						return true
					}
				}
			case map[string]interface{}:
				if containKeysInMapItemFields(result, searchFields, matchedKeys...) {
					return true
				}
			}
		}
	}
	return false
}

func joinMultiLineString(lines string) string {
	lines = strings.ReplaceAll(lines, "\r", "\n")
	lines = strings.ReplaceAll(lines, "\n\n", "\n")
	lineArray := strings.Split(strings.TrimSpace(lines), "\n")
	lineSlice := make([]string, 0, len(lineArray))
	for _, item := range lineArray {
		cleanLine := strings.TrimSpace(item)
		if cleanLine == "" {
			continue
		}
		lineSlice = append(lineSlice, strings.ReplaceAll(cleanLine, " ", ","))
	}
	return strings.Join(lineSlice, "|")
}

func getUniqueAssetFromKey(key string, currentResult []model.Asset) (data model.Asset, ok bool) {
	result := make([]int, 0, len(currentResult))
	for i := range currentResult {
		asset := currentResult[i]
		ip := asset.Address
		hostname := asset.Name
		switch key {
		case ip, hostname:
			result = append(result, i)
		}
	}
	if len(result) == 1 {
		return currentResult[result[0]], true
	}
	return model.Asset{}, false
}
