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

type AssetEngine interface {
	baseEngine
	Retrieve(pageSize, offset int, searches ...string) []model.Asset
}

var (
	_ Application = (*AssetApplication)(nil)

	_ AssetEngine = (*remoteAssetEngine)(nil)

	_ AssetEngine = (*localAssetEngine)(nil)

	_ AssetEngine = (*remoteNodeAssetEngine)(nil)
)

type AssetApplication struct {
	h *interactiveHandler

	engine AssetEngine

	searchKeys []string

	currentResult []model.Asset
}

func (k *AssetApplication) Name() string {
	return "Asset"
}

func (k *AssetApplication) MoveNextPage() {
	if k.engine.HasNext() {
		offset := k.engine.CurrentOffSet()
		newPageSize := getPageSize(k.h.term)
		currentAssets := k.engine.Retrieve(newPageSize, offset, k.searchKeys...)
		k.currentResult = model.AssetList(currentAssets).SortBy(config.GetConf().AssetListSortBy)
	}
	k.DisplayCurrentResult()
}

func (k *AssetApplication) MovePrePage() {
	if k.engine.HasPrev() {
		offset := k.engine.CurrentOffSet()
		newPageSize := getPageSize(k.h.term)
		start := offset - newPageSize*2
		if start <= 0 {
			start = 0
		}
		currentAssets := k.engine.Retrieve(newPageSize, start, k.searchKeys...)
		k.currentResult = model.AssetList(currentAssets).SortBy(config.GetConf().AssetListSortBy)
	}
	k.DisplayCurrentResult()
}

func (k *AssetApplication) Search(key string) {
	newPageSize := getPageSize(k.h.term)
	k.searchKeys = []string{key}
	currentAssets := k.engine.Retrieve(newPageSize, 0, key)
	k.currentResult = model.AssetList(currentAssets).SortBy(config.GetConf().AssetListSortBy)
	k.DisplayCurrentResult()
}

func (k *AssetApplication) SearchAgain(key string) {
	k.searchKeys = append(k.searchKeys, key)
	newPageSize := getPageSize(k.h.term)
	currentAssets := k.engine.Retrieve(newPageSize, 0, k.searchKeys...)
	k.currentResult = model.AssetList(currentAssets).SortBy(config.GetConf().AssetListSortBy)
	k.DisplayCurrentResult()
}

func (k *AssetApplication) SearchOrProxy(key string) {
	if indexNum, err := strconv.Atoi(key); err == nil && len(k.currentResult) > 0 {
		if indexNum > 0 && indexNum <= len(k.currentResult) {
			k.proxyAsset(k.currentResult[indexNum-1])
			return
		}
	}

	newPageSize := getPageSize(k.h.term)
	currentResult := k.engine.Retrieve(newPageSize, 0, key)
	if len(currentResult) == 1 {
		k.proxyAsset(currentResult[0])
		return
	}
	k.currentResult = currentResult
	k.searchKeys = []string{key}
	k.DisplayCurrentResult()
}

func (k *AssetApplication) DisplayCurrentResult() {
	currentAssets := k.currentResult
	term := k.h.term
	searchHeader := fmt.Sprintf(i18n.T("Search: %s"), strings.Join(k.searchKeys, " "))

	if len(currentAssets) == 0 {
		noAssets := i18n.T("No Assets")
		switch v := k.engine.(type) {
		case *remoteNodeAssetEngine:
			noAssets = fmt.Sprintf(i18n.T("%s node has no assets"), v.node.Name)
		}
		utils.IgnoreErrWriteString(term, utils.WrapperString(noAssets, utils.Red))
		utils.IgnoreErrWriteString(term, utils.CharNewLine)
		utils.IgnoreErrWriteString(term, utils.WrapperString(searchHeader, utils.Green))
		utils.IgnoreErrWriteString(term, utils.CharNewLine)
		return
	}

	currentPage := k.engine.CurrentPage()
	pageSize := k.engine.PageSize()
	totalPage := k.engine.TotalPage()
	totalCount := k.engine.TotalCount()

	idLabel := i18n.T("ID")
	hostLabel := i18n.T("Hostname")
	ipLabel := i18n.T("IP")
	commentLabel := i18n.T("Comment")

	Labels := []string{idLabel, hostLabel, ipLabel, commentLabel}
	fields := []string{"ID", "hostname", "IP", "comment"}
	data := make([]map[string]string, len(currentAssets))
	for i, j := range currentAssets {
		row := make(map[string]string)
		row["ID"] = strconv.Itoa(i + 1)
		row["hostname"] = j.Hostname
		row["IP"] = j.IP

		comments := make([]string, 0)
		for _, item := range strings.Split(strings.TrimSpace(j.Comment), "\r\n") {
			if strings.TrimSpace(item) == "" {
				continue
			}
			comments = append(comments, strings.ReplaceAll(strings.TrimSpace(item), " ", ","))
		}
		row["comment"] = strings.Join(comments, "|")
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
			"hostname": {0, 8, 0},
			"IP":       {0, 15, 40},
			"comment":  {0, 0, 0},
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

func (k *AssetApplication) proxyAsset(assetSelect model.Asset) {
	systemUsers := service.GetUserAssetSystemUsers(k.h.user.ID, assetSelect.ID)
	defer k.h.term.SetPrompt("[Host]> ")
	systemUserSelect, ok := k.h.chooseSystemUser(systemUsers)
	if !ok {
		return
	}
	k.proxy(assetSelect, systemUserSelect)
}

func (k *AssetApplication) proxy(assetSelect model.Asset, systemUserSelect model.SystemUser) {
	p := proxy.ProxyServer{
		UserConn:   k.h.sess,
		User:       k.h.user,
		Asset:      &assetSelect,
		SystemUser: &systemUserSelect,
	}
	k.h.pauseWatchWinSize()
	p.Proxy()
	k.h.resumeWatchWinSize()
	logger.Infof("Request %s: asset %s proxy end", k.h.sess.Uuid, assetSelect.Hostname)
}

type localAssetEngine struct {
	data []model.Asset
	*pageInfo

	cacheLastSearchResult []model.Asset
	cacheLastSearchKeys   []string
}

func (e *localAssetEngine) Retrieve(pageSize, offset int, searches ...string) (Assets []model.Asset) {
	if pageSize <= 0 {
		pageSize = PAGESIZEALL
	}
	if offset < 0 {
		offset = 0
	}

	searchResult := e.searchResult(searches...)
	var (
		totalAsset      []model.Asset
		total           int
		currentOffset   int
		currentPageSize int
	)

	if offset < len(searchResult) {
		totalAsset = searchResult[offset:]
	}
	total = len(totalAsset)
	currentPageSize = pageSize
	if currentPageSize == PAGESIZEALL {
		currentPageSize = len(totalAsset)
	}
	if total > currentPageSize {
		Assets = totalAsset[:currentPageSize]
	} else {
		Assets = totalAsset
	}
	currentOffset = offset + len(Assets)
	e.updatePageInfo(currentPageSize, total, currentOffset)
	return
}

func (e *localAssetEngine) searchResult(searches ...string) []model.Asset {
	if len(searches) == 0 {
		return e.data
	}
	if len(searches) == 1 && searches[0] == "" {
		return e.data
	}
	sort.Strings(searches)
	if IsEqualStringSlice(e.cacheLastSearchKeys, searches) &&
		e.cacheLastSearchResult != nil {
		return e.cacheLastSearchResult
	}
	e.cacheLastSearchKeys = searches
	e.cacheLastSearchResult = searchMatchedAssets(e.data, searches...)
	return e.cacheLastSearchResult
}

func (e *localAssetEngine) HasPrev() bool {
	return e.currentPage > 1
}

func (e *localAssetEngine) HasNext() bool {
	return e.currentPage < e.totalPage
}

type remoteAssetEngine struct {
	user *model.User
	*pageInfo

	nextUrl string
	preUrl  string
}

func (e *remoteAssetEngine) Retrieve(pageSize, offset int, searches ...string) (Assets []model.Asset) {
	resp := service.GetUserAssets(e.user.ID, pageSize, offset, searches...)
	var (
		total           int
		currentOffset   int
		currentPageSize int
	)
	e.nextUrl = resp.NextURL
	e.preUrl = resp.PreviousURL
	Assets = resp.Data
	total = resp.Total
	currentPageSize = pageSize

	if currentPageSize < 0 || currentPageSize == PAGESIZEALL {
		currentPageSize = len(Assets)
	}
	if len(Assets) > currentPageSize {
		Assets = Assets[:currentPageSize]
	}
	currentOffset = offset + len(Assets)
	e.updatePageInfo(currentPageSize, total, currentOffset)
	return
}

func (e *remoteAssetEngine) HasPrev() bool {
	return e.preUrl != ""
}

func (e *remoteAssetEngine) HasNext() bool {
	return e.nextUrl != ""
}

type remoteNodeAssetEngine struct {
	user    *model.User
	node    model.Node
	nextUrl string
	preUrl  string
	*pageInfo
}

func (e *remoteNodeAssetEngine) Retrieve(pageSize, offset int, searches ...string) (Assets []model.Asset) {
	resp := service.GetUserNodePaginationAssets(e.user.ID, e.node.ID, pageSize, offset, searches...)
	var (
		total           int
		currentOffset   int
		currentPageSize int
	)
	e.nextUrl = resp.NextURL
	e.preUrl = resp.PreviousURL
	Assets = resp.Data
	total = resp.Total
	currentPageSize = pageSize

	if currentPageSize < 0 || currentPageSize == PAGESIZEALL {
		currentPageSize = len(Assets)
	}

	if len(Assets) > currentPageSize {
		Assets = Assets[:currentPageSize]
	}
	currentOffset = offset + len(Assets)
	e.updatePageInfo(currentPageSize, total, currentOffset)
	return
}

func (e *remoteNodeAssetEngine) HasPrev() bool {
	return e.preUrl != ""
}

func (e *remoteNodeAssetEngine) HasNext() bool {
	return e.nextUrl != ""
}

func searchMatchedAssets(data []model.Asset, keys ...string) []model.Asset {
	matched := make([]model.Asset, 0, len(data))
	for i := range data {
		asset := data[i]
		ok := true
		contents := []string{strings.ToLower(asset.Hostname),
			strings.ToLower(asset.IP), strings.ToLower(asset.Comment)}

		for j := range keys {
			if !isSubstring(contents, strings.ToLower(keys[j])) {
				ok = false
				break
			}
		}
		if ok {
			matched = append(matched, asset)
		}
	}
	return matched
}
