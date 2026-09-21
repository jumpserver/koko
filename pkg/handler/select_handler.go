package handler

import (
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/jumpserver-dev/sdk-go/model"
	"github.com/jumpserver/koko/pkg/i18n"
	"github.com/jumpserver/koko/pkg/logger"
	"github.com/jumpserver/koko/pkg/srvconn"
	"github.com/jumpserver/koko/pkg/utils"
)

type dataSource string

const (
	loadingFromLocal  dataSource = "local"
	loadingFromRemote dataSource = "remote"
)

type prompt string

const (
	promptAsset    prompt = "[Asset]> "
	promptHost     prompt = "[Host]> "
	promptK8s      prompt = "[K8S]> "
	promptDatabase prompt = "[DB]> "
)

type selectType int

const (
	TypeAsset selectType = iota + 1
	TypeNodeAsset
	TypeTypeAsset
	TypeFavoriteAsset
	TypeK8s
	TypeDatabase
	TypeHost
)

type UserSelectHandler struct {
	user *model.User
	h    *InteractiveHandler

	loadingPolicy dataSource
	currentType   selectType
	promptStr     prompt
	searchKeys    []string

	hasPre  bool
	hasNext bool

	allLocalData []model.PermAsset

	selectedNode     model.Node
	selectedType     classicTypeNode
	selectedFavorite classicFavoriteNode
	selectedPath     string
	currentResult    []model.PermAsset
	connectable      []bool

	*pageInfo

	selectedAsset   *model.PermAsset
	selectedAccount *model.PermAccount

	hiddenFields map[string]struct{}
}

func (u *UserSelectHandler) SetSelectType(selection selectType) {
	u.SetLoadPolicy(loadingFromRemote)
	u.currentType = selection
	switch selection {
	case TypeAsset:
		if u.h.assetLoadPolicy == "all" {
			u.SetLoadPolicy(loadingFromLocal)
			u.AutoCompletion()
		}
		u.promptStr = promptAsset
	case TypeNodeAsset:
		u.promptStr = promptAsset
	case TypeTypeAsset, TypeFavoriteAsset:
		u.promptStr = promptAsset
	case TypeHost:
		u.promptStr = promptHost
	case TypeK8s:
		u.promptStr = promptK8s
	case TypeDatabase:
		u.promptStr = promptDatabase
	}
	u.h.term.SetPrompt(string(u.promptStr))
}

func (u *UserSelectHandler) AutoCompletion() {
	assets := u.Retrieve(0, 0, "")
	suggests := make([]string, 0, len(assets))
	for _, asset := range assets {
		suggests = append(suggests, asset.Name)
	}
	sort.Strings(suggests)
	u.h.term.AutoCompleteCallback = func(line string, pos int, key rune) (newLine string, newPos int, ok bool) {
		if key == 9 {
			termWidth, _ := u.h.GetPtySize()
			if len(line) >= 1 {
				matches := utils.FilterPrefix(suggests, line)
				if len(matches) >= 1 {
					commonPrefix := utils.LongestCommonPrefix(matches)
					switch u.currentType {
					case TypeAsset, TypeNodeAsset, TypeTypeAsset, TypeFavoriteAsset:
						fmt.Fprintf(u.h.term, "%s%s\n%s\n", promptAsset, line, utils.Pretty(matches, termWidth))
					case TypeK8s:
						fmt.Fprintf(u.h.term, "%s%s\n%s\n", promptK8s, line, utils.Pretty(matches, termWidth))
					case TypeDatabase:
						fmt.Fprintf(u.h.term, "%s%s\n%s\n", promptDatabase, line, utils.Pretty(matches, termWidth))
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
	u.selectedPath = nodeDisplayPath(u.h.nodes, node)
}

func (u *UserSelectHandler) SetType(node classicTypeNode) {
	u.SetSelectType(TypeTypeAsset)
	u.selectedType = node
	u.selectedPath = node.Path
}

func (u *UserSelectHandler) SetFavorite(node classicFavoriteNode) {
	u.SetSelectType(TypeFavoriteAsset)
	u.selectedFavorite = node
	u.selectedPath = node.Path
}

func (u *UserSelectHandler) SetAllLocalData(data []model.PermAsset) {
	u.allLocalData = make([]model.PermAsset, len(data))
	copy(u.allLocalData, data)
}

func (u *UserSelectHandler) SetLoadPolicy(policy dataSource) { u.loadingPolicy = policy }

func (u *UserSelectHandler) MoveNextPage() {
	if u.HasNext() {
		offset := u.CurrentOffSet()
		pageSize := u.resultPageSize()
		u.currentResult = u.Retrieve(pageSize, offset, u.searchKeys...)
	}
	u.DisplayCurrentResult()
}

func (u *UserSelectHandler) MovePrePage() {
	if u.HasPrev() {
		pageSize := u.resultPageSize()
		offset := previousPageOffset(u.CurrentOffSet(), len(u.currentResult), pageSize)
		u.currentResult = u.Retrieve(pageSize, offset, u.searchKeys...)
	}
	u.DisplayCurrentResult()
}

func previousPageOffset(currentOffset, currentCount, pageSize int) int {
	return max(currentOffset-currentCount-pageSize, 0)
}

func (u *UserSelectHandler) Search(key string) {
	key = normalizeClassicSearchKey(key)
	pageSize := u.resultPageSize()
	u.searchKeys = nil
	if key != "" {
		u.searchKeys = []string{key}
	}
	u.currentResult = u.Retrieve(pageSize, 0, u.searchKeys...)
	u.DisplayCurrentResult()
}

func (u *UserSelectHandler) SearchAgain(key string) {
	if key = normalizeClassicSearchKey(key); key != "" {
		u.searchKeys = append(u.searchKeys, key)
	}
	pageSize := u.resultPageSize()
	u.currentResult = u.Retrieve(pageSize, 0, u.searchKeys...)
	u.DisplayCurrentResult()
}

func normalizeClassicSearchKey(key string) string {
	key = strings.TrimSpace(key)
	if strings.HasPrefix(key, "+") {
		key = strings.TrimSpace(key[1:])
	}
	return key
}

func (u *UserSelectHandler) resultPageSize() int {
	pageSize := getPageSize(u.h, u.h.terminalConf)
	if pageSize <= 1 {
		return pageSize
	}
	configured := u.h.terminalConf.AssetListPageSize
	if configured == "all" {
		return pageSize
	}
	if _, err := strconv.Atoi(configured); err == nil {
		return pageSize
	}
	hintCount := 4
	if u.currentType == TypeNodeAsset || u.currentType == TypeTypeAsset || u.currentType == TypeFavoriteAsset {
		hintCount++
	}
	_, height := u.h.GetPtySize()
	// Keep the context lines, compact action panel, two asset-table header lines, and input prompt visible.
	availableRows := height - hintCount - 4
	return max(1, min(pageSize, availableRows))
}

func (u *UserSelectHandler) SelectResult(key string) bool {
	index, ok := u.currentResultIndex(key)
	if !ok {
		return false
	}
	if !u.assetCanConnect(index) {
		u.showUnavailableAsset(u.currentResult[index])
		return true
	}
	u.Proxy(u.currentResult[index])
	return true
}

func (u *UserSelectHandler) currentResultIndex(key string) (int, bool) {
	number, err := strconv.Atoi(key)
	if err != nil || len(u.currentResult) == 0 {
		return 0, false
	}
	firstNumber, _ := resultDisplayRange(u.CurrentOffSet(), len(u.currentResult), u.TotalCount())
	index := number - firstNumber
	return index, index >= 0 && index < len(u.currentResult)
}

func (u *UserSelectHandler) HasPrev() bool { return u.hasPre }
func (u *UserSelectHandler) HasNext() bool { return u.hasNext }

func (u *UserSelectHandler) DisplayCurrentResult() {
	searchHeader := fmt.Sprintf(i18n.NewLang(u.h.i18nLang).T("Search: %s"), strings.Join(u.searchKeys, " "))
	switch u.currentType {
	case TypeDatabase:
		u.displayDatabaseResult(searchHeader)
	case TypeK8s:
		u.displayK8sResult(searchHeader)
	case TypeNodeAsset:
		u.displayNodeAssetResult(searchHeader)
	case TypeTypeAsset, TypeFavoriteAsset:
		u.displayAssetResult(searchHeader)
	case TypeAsset, TypeHost:
		u.displayAssetResult(searchHeader)
	default:
		logger.Error("Display unknown type")
	}
}

func (u *UserSelectHandler) Proxy(target model.PermAsset) {
	u.proxyAsset(target)
	u.h.term.SetPrompt(string(u.promptStr))
}

func (u *UserSelectHandler) Retrieve(pageSize, offset int, searches ...string) []model.PermAsset {
	if u.loadingPolicy == loadingFromLocal {
		return u.retrieveFromLocal(pageSize, offset, searches...)
	}
	return u.retrieveFromRemote(pageSize, offset, searches...)
}

func (u *UserSelectHandler) retrieveFromLocal(pageSize, offset int, searches ...string) []model.PermAsset {
	if pageSize <= 0 {
		pageSize = PAGESIZEALL
	}
	offset = max(offset, 0)
	searchResult := u.retrieveLocal(searches...)
	var totalData []model.PermAsset
	if offset < len(searchResult) {
		totalData = searchResult[offset:]
	}
	total := len(searchResult)
	currentPageSize := pageSize
	currentData := totalData
	if currentPageSize < 0 || currentPageSize == PAGESIZEALL {
		currentPageSize = len(totalData)
	}
	if len(totalData) > currentPageSize {
		currentData = totalData[:currentPageSize]
	}
	currentData = u.prepareAssetPage(currentData, u.assetListPath())
	currentOffset := offset + len(currentData)
	u.updatePageInfo(currentPageSize, total, currentOffset)
	u.hasPre = u.currentPage > 1
	u.hasNext = u.currentPage < u.totalPage
	return currentData
}

func (u *UserSelectHandler) retrieveLocal(searches ...string) []model.PermAsset {
	switch u.currentType {
	case TypeAsset, TypeHost, TypeDatabase, TypeK8s:
		return u.searchLocalAsset(searches...)
	case TypeTypeAsset:
		return u.searchLocalTypeAsset(searches...)
	default:
		u.SetSelectType(TypeAsset)
		logger.Info("Retrieve default local data type: Asset")
		return u.searchLocalAsset(searches...)
	}
}

func (u *UserSelectHandler) searchLocalFromFields(fields map[string]struct{}, searches ...string) []model.PermAsset {
	items := make([]model.PermAsset, 0, len(u.allLocalData))
	for i := range u.allLocalData {
		asset := u.allLocalData[i]
		data := map[string]interface{}{
			"name": asset.Name, "address": asset.Address, "org_name": asset.OrgName,
			"platform": asset.Platform.Name, "comment": asset.Comment,
		}
		if containKeysInMapItemFields(data, fields, searches...) {
			items = append(items, asset)
		}
	}
	return items
}

func (u *UserSelectHandler) retrieveFromRemote(pageSize, offset int, searches ...string) []model.PermAsset {
	order := "name"
	if u.h.terminalConf.AssetListSortBy == "ip" {
		order = "address"
	}
	reqParam := model.PaginationParam{
		PageSize: pageSize, Offset: offset, Searches: searches, Order: order,
	}
	switch u.currentType {
	case TypeNodeAsset:
		return u.retrieveRemoteNodeAsset(reqParam)
	case TypeTypeAsset:
		reqParam.Category = u.selectedType.Category
		reqParam.Type = u.selectedType.AssetType
		return u.retrieveRemoteAsset(reqParam)
	case TypeFavoriteAsset:
		return u.retrieveRemoteFavoriteAsset(reqParam)
	case TypeAsset, TypeHost, TypeDatabase, TypeK8s:
		return u.retrieveRemoteAsset(reqParam)
	default:
		u.SetSelectType(TypeAsset)
		logger.Info("Retrieve default remote data type: Asset")
		return u.retrieveRemoteAsset(reqParam)
	}
}

func (u *UserSelectHandler) assetListPath() string {
	if u.user == nil {
		return ""
	}
	data := tuiData{userID: u.user.ID}
	if u.currentType == TypeFavoriteAsset {
		return data.userPath("nodes/favorite/assets/")
	}
	if u.currentType != TypeNodeAsset {
		return data.userPath("assets/")
	}
	switch u.selectedNode.ID {
	case "ungrouped":
		return data.userPath("nodes/ungrouped/assets/")
	case "favorite":
		return data.userPath("nodes/favorite/assets/")
	default:
		return data.userPath("nodes/" + url.PathEscape(u.selectedNode.ID) + "/assets/")
	}
}

func (u *UserSelectHandler) prepareAssetPage(assets []model.PermAsset, path string) []model.PermAsset {
	u.connectable = nil
	if len(assets) == 0 {
		return assets
	}
	if path == "" || u.h == nil || u.h.jmsService == nil {
		u.connectable = make([]bool, len(assets))
		for i := range assets {
			u.connectable[i] = assets[i].IsActive
		}
		return assets
	}
	ids := make([]string, 0, len(assets))
	for _, asset := range assets {
		ids = append(ids, asset.ID)
	}
	protocols := srvconn.SupportedProtocols()
	sort.Strings(protocols)
	var supported model.PaginationResponse
	_, err := u.h.jmsService.Call("GET", path, nil, &supported, map[string]string{
		"limit":     strconv.Itoa(len(ids)),
		"offset":    "0",
		"id__in":    strings.Join(ids, ","),
		"protocols": strings.Join(protocols, ","),
	})
	if err != nil {
		logger.Errorf("Classic text asset protocol support lookup failed: %s", err)
	}
	supportedIDs := make(map[string]struct{}, len(supported.Data))
	for _, asset := range supported.Data {
		supportedIDs[asset.ID] = struct{}{}
	}
	ordered := make([]model.PermAsset, 0, len(assets))
	availability := make([]bool, 0, len(assets))
	for _, enabled := range []bool{true, false} {
		for _, asset := range assets {
			_, protocolSupported := supportedIDs[asset.ID]
			canConnect := asset.IsActive && protocolSupported
			if canConnect != enabled {
				continue
			}
			ordered = append(ordered, asset)
			availability = append(availability, canConnect)
		}
	}
	u.connectable = availability
	return ordered
}

func (u *UserSelectHandler) assetCanConnect(index int) bool {
	return index >= 0 && index < len(u.connectable) && u.connectable[index]
}

func (u *UserSelectHandler) updateRemotePageData(reqParam model.PaginationParam,
	response model.PaginationResponse) []model.PermAsset {
	u.hasNext = response.NextURL != ""
	u.hasPre = response.PreviousURL != ""
	currentPageSize := reqParam.PageSize
	currentData := response.Data
	if currentPageSize < 0 || currentPageSize == PAGESIZEALL {
		currentPageSize = len(response.Data)
	}
	if len(response.Data) > currentPageSize {
		currentData = currentData[:currentPageSize]
	}
	u.updatePageInfo(currentPageSize, response.Total, reqParam.Offset+len(currentData))
	return currentData
}

func containKeysInMapItemFields(item map[string]interface{}, searchFields map[string]struct{}, matchedKeys ...string) bool {
	if len(matchedKeys) == 0 || len(matchedKeys) == 1 && matchedKeys[0] == "" {
		return true
	}
	for key, value := range item {
		if _, ok := searchFields[key]; !ok {
			continue
		}
		switch result := value.(type) {
		case string:
			for _, matchedKey := range matchedKeys {
				if strings.Contains(result, matchedKey) {
					return true
				}
			}
		case map[string]interface{}:
			if containKeysInMapItemFields(result, searchFields, matchedKeys...) {
				return true
			}
		}
	}
	return false
}
