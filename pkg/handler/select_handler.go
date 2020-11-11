package handler

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/jumpserver/koko/pkg/i18n"
	"github.com/jumpserver/koko/pkg/logger"
	"github.com/jumpserver/koko/pkg/model"
	"github.com/jumpserver/koko/pkg/service"
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
	TypeMySQL
)

type UserSelectHandler struct {
	user *model.User
	h    *interactiveHandler

	loadingPolicy dataSource
	currentType   selectType
	searchKeys    []string

	hasPre  bool
	hasNext bool

	allLocalData []map[string]interface{}

	selectedNode  model.Node
	currentResult []map[string]interface{}

	*pageInfo
}

func (u *UserSelectHandler) SetSelectType(s selectType) {
	u.SetLoadPolicy(loadingFromRemote) // default remote
	switch s {
	case TypeAsset:
		switch u.h.assetLoadPolicy {
		case "all":
			u.SetLoadPolicy(loadingFromLocal)
		}
		u.h.term.SetPrompt("[Host]> ")
	case TypeNodeAsset:
		u.h.term.SetPrompt("[Host]> ")
	case TypeK8s:
		u.h.term.SetPrompt("[K8S]> ")
	case TypeMySQL:
		u.h.term.SetPrompt("[DB]> ")
	}
	u.currentType = s
}

func (u *UserSelectHandler) SetNode(node model.Node) {
	u.SetSelectType(TypeNodeAsset)
	u.selectedNode = node
}

func (u *UserSelectHandler) SetAllLocalData(data []map[string]interface{}) {
	// 使用副本
	u.allLocalData = make([]map[string]interface{}, len(data))
	copy(u.allLocalData, data)
}

func (u *UserSelectHandler) SetLoadPolicy(policy dataSource) {
	u.loadingPolicy = policy
}

func (u *UserSelectHandler) MoveNextPage() {
	if u.HasNext() {
		offset := u.CurrentOffSet()
		newPageSize := getPageSize(u.h.term)
		u.currentResult = u.Retrieve(newPageSize, offset, u.searchKeys...)
	}
	u.DisplayCurrentResult()
}

func (u *UserSelectHandler) MovePrePage() () {
	if u.HasPrev() {
		offset := u.CurrentOffSet()
		newPageSize := getPageSize(u.h.term)
		u.currentResult = u.Retrieve(newPageSize, offset, u.searchKeys...)
	}
	u.DisplayCurrentResult()
}

func (u *UserSelectHandler) Search(key string) () {
	newPageSize := getPageSize(u.h.term)
	u.currentResult = u.Retrieve(newPageSize, 0, key)
	u.searchKeys = []string{key}
	u.DisplayCurrentResult()
}

func (u *UserSelectHandler) SearchAgain(key string) () {
	u.searchKeys = append(u.searchKeys, key)
	newPageSize := getPageSize(u.h.term)
	u.currentResult = u.Retrieve(newPageSize, 0, u.searchKeys...)
	u.DisplayCurrentResult()
}

func (u *UserSelectHandler) SearchOrProxy(key string) () {
	if indexNum, err := strconv.Atoi(key); err == nil && len(u.currentResult) > 0 {
		if indexNum > 0 && indexNum <= len(u.currentResult) {
			u.Proxy(u.currentResult[indexNum-1])
			return
		}
	}

	newPageSize := getPageSize(u.h.term)
	currentResult := u.Retrieve(newPageSize, 0, key)
	u.currentResult = currentResult
	u.searchKeys = []string{key}
	if len(currentResult) == 1 {
		u.Proxy(currentResult[0])
		return
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
	searchHeader := fmt.Sprintf(i18n.T("Search: %s"), strings.Join(u.searchKeys, " "))
	switch u.currentType {
	case TypeMySQL:
		u.displayMySQLResult(searchHeader)
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

func (u *UserSelectHandler) Proxy(target map[string]interface{}) {
	targetId := target["id"].(string)
	switch u.currentType {
	case TypeAsset, TypeNodeAsset:
		asset := service.GetAsset(targetId)
		if asset.ID == "" {
			logger.Errorf("Select asset %s not found", targetId)
			return
		}
		u.proxyAsset(asset)
	case TypeK8s:
		app := service.GetK8sApplication(targetId)
		if app.Id == "" {
			logger.Errorf("Select k8s %s not found", targetId)
			return
		}
		u.proxyK8s(app)
	case TypeMySQL:
		app := service.GetMySQLApplication(targetId)
		if app.Id == "" {
			logger.Errorf("Select MySQL %s not found", targetId)
			return
		}
		u.proxyMySQL(app)
	default:
		logger.Errorf("Select unknown type for target id %s", targetId)
	}
}

func (u *UserSelectHandler) Retrieve(pageSize, offset int, searches ...string) []map[string]interface{} {
	switch u.loadingPolicy {
	case loadingFromLocal:
		return u.retrieveFromLocal(pageSize, offset, searches...)
	default:
		return u.retrieveFromRemote(pageSize, offset, searches...)
	}
}

func (u *UserSelectHandler) retrieveFromLocal(pageSize, offset int, searches ...string) []map[string]interface{} {
	if pageSize <= 0 {
		pageSize = PAGESIZEALL
	}
	if offset < 0 {
		offset = 0
	}

	searchResult := u.retrieveLocal(searches...)
	var (
		totalData       []map[string]interface{}
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

func (u *UserSelectHandler) retrieveLocal(searches ...string) []map[string]interface{} {
	switch u.currentType {
	case TypeMySQL:
		return u.searchLocalMySQL(searches...)
	case TypeK8s:
		return u.searchLocalK8s(searches...)
	default:
		// TypeAsset
		return u.searchLocalAsset(searches...)
	}
}

func (u *UserSelectHandler) searchLocalFromFields(fields map[string]struct{}, searches ...string) []map[string]interface{} {
	items := make([]map[string]interface{}, 0, len(u.allLocalData))
	for i := range u.allLocalData {
		if containKeysInMapItemFields(u.allLocalData[i], fields, searches...) {
			items = append(items, u.allLocalData[i])
		}
	}
	return items
}

func (u *UserSelectHandler) retrieveFromRemote(pageSize, offset int, searches ...string) []map[string]interface{} {
	reqParam := model.PaginationParam{
		PageSize: pageSize,
		Offset:   offset,
		Searches: searches,
	}
	switch u.currentType {
	case TypeMySQL:
		return u.retrieveRemoteMySQL(reqParam)
	case TypeK8s:
		return u.retrieveRemoteK8s(reqParam)
	case TypeNodeAsset:
		return u.retrieveRemoteNodeAsset(reqParam)
	case TypeAsset:
		return u.retrieveRemoteAsset(reqParam)
	default:
		// TypeAsset
		u.SetSelectType(TypeAsset)
		logger.Info("Retrieve default data type: Asset")
		return u.retrieveRemoteAsset(reqParam)
	}
}

func (u *UserSelectHandler) updateRemotePageData(reqParam model.PaginationParam,
	res model.PaginationResponse) []map[string]interface{} {
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
			switch value.(type) {
			case string:
				result := value.(string)
				for i := range matchedKeys {
					if strings.Contains(result, matchedKeys[i]) {
						return true
					}
				}
			case map[string]interface{}:
				result := value.(map[string]interface{})
				if containKeysInMapItemFields(result, searchFields, matchedKeys...) {
					return true
				}
			}
		}
	}
	return false
}

func convertMapItemToRow(item map[string]interface{}, fields map[string]string, row map[string]string) map[string]string {
	for key, value := range item {
		if rowKey, ok := fields[key]; ok {
			switch value.(type) {
			case string:
				row[rowKey] = value.(string)
			case int:
				row[rowKey] = strconv.Itoa(value.(int))
			}
			continue
		}
		switch value.(type) {
		case map[string]interface{}:
			row = convertMapItemToRow(value.(map[string]interface{}), fields, row)
		}
	}
	return row
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
