package handler

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/jumpserver/koko/pkg/common"
	"github.com/jumpserver/koko/pkg/config"
	"github.com/jumpserver/koko/pkg/logger"
	"github.com/jumpserver/koko/pkg/model"
	"github.com/jumpserver/koko/pkg/service"
	"github.com/jumpserver/koko/pkg/utils"
)

type AssetSource string

const (
	FromPem  AssetSource = "FromPem"
	FromNode AssetSource = "FromNode"
)

type remotePaginator struct {
	offset int
	limit  int
	search string
	Data   model.AssetsPaginationResponse

	dataSource  AssetSource
	currentNode model.Node
	initialed   bool
}

func (r *remotePaginator) retrievePageAssets(userID string) []model.Asset {
	if r.limit == 0 || r.offset < 0 || r.limit >= r.Data.Total {
		r.offset = 0
	}
	r.Data = service.GetUserAssets(userID, r.search, r.limit, r.offset)
	return r.Data.Data
}

func (r *remotePaginator) retrieveNodeAssets(userID, nodeID string) []model.Asset {
	if r.limit == 0 || r.offset < 0 || r.limit >= r.Data.Total {
		r.offset = 0
	}
	r.Data = service.GetUserNodePaginationAssets(userID, nodeID, r.search, r.limit, r.offset)
	return r.Data.Data
}

func (r *remotePaginator) SetPageSize(size int) {
	r.limit = size

}
func (r *remotePaginator) HasPrev() bool {
	return r.Data.PreviousURL != ""
}

func (r *remotePaginator) HasNext() bool {
	return r.Data.NextURL != ""
}

func (h *interactiveHandler) NewDispatch() {
	for {
		line, err := h.term.ReadLine()
		if err != nil {
			logger.Debugf("User %s close connect", h.user.Name)
			break
		}
		line = strings.TrimSpace(line)
		h.once.Do(h.initPaginator)
		h.setPageSize()
		switch len(line) {
		case 0, 1:
			switch strings.ToLower(line) {
			case "p":
				// 展示上一页资产
				h.movePrePage()
			case "", "n":
				// 展示下一页资产
				h.moveNextPage()
			case "g":
				// 展示节点
				<-h.loadDataDone
				h.displayNodes(h.nodes)
				continue
			case "h":
				h.displayBanner()
				continue
			case "r":
				h.refreshAssetsAndNodesData()
				continue
			case "q":
				logger.Debugf("user %s enter to exit", h.user.Name)
				return
			default:
				if ok := h.searchOrProxy(line); ok {
					continue
				}
			}
		default:
			switch {
			case line == "exit", line == "quit":
				logger.Debugf("user %s enter to exit", h.user.Name)
				return
			case strings.Index(line, "/") == 0:
				searchWord := strings.TrimSpace(line[1:])
				h.searchAsset(searchWord)
			case strings.Index(line, "g") == 0:
				searchWord := strings.TrimSpace(strings.TrimPrefix(line, "g"))
				if num, err := strconv.Atoi(searchWord); err == nil {
					if num >= 0 {
						h.searchNewNodeAssets(num)
						//h.displayAssets(assets)
						break
					}
				}
				if ok := h.searchOrProxy(line); ok {
					continue
				}
			default:
				if ok := h.searchOrProxy(line); ok {
					continue
				}
			}
		}

		h.displayPageAssets()
	}
}

func (h *interactiveHandler) displayPageAssets() {
	if len(h.currentData) == 0 {
		_, _ = h.term.Write([]byte(getI18nFromMap("NoAssets") + "\n\r"))
		return
	}
	Labels := []string{getI18nFromMap("ID"), getI18nFromMap("Hostname"),
		getI18nFromMap("IP"), getI18nFromMap("Comment")}
	fields := []string{"ID", "hostname", "IP", "comment"}
	h.currentSortedData = model.AssetList(h.currentData).SortBy(config.GetConf().AssetListSortBy)
	data := make([]map[string]string, len(h.currentSortedData))
	for i, j := range h.currentSortedData {
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
	w, _ := h.term.GetSize()
	currentPage, pageSize, totalPage, totalCount := h.getPageBasicInfo()
	caption := fmt.Sprintf(getI18nFromMap("AssetTableCaption"),
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

	_, _ = h.term.Write([]byte(utils.CharClear))
	_, _ = h.term.Write([]byte(table.Display()))
}

func (h *interactiveHandler) movePrePage() {

	switch h.assetLoadPolicy {
	case "all":
		if !h.localPaginator.HasPrev() {
			return
		}
		prePageData := h.localPaginator.GetPrevPageData()
		if len(h.currentData) != len(prePageData) {
			h.currentData = make([]model.Asset, len(prePageData))
		}
		for i, item := range prePageData {
			h.currentData[i] = item.(model.Asset)
		}
	default:
		if h.remotePaginator.initialed && !h.remotePaginator.HasPrev() {
			return
		}
		h.remotePaginator.initialed = true
		h.remotePaginator.offset -= h.remotePaginator.limit
		switch h.remotePaginator.dataSource {
		case FromNode:
			h.currentData = h.remotePaginator.retrieveNodeAssets(h.user.ID, h.remotePaginator.currentNode.ID)
		default:
			h.currentData = h.remotePaginator.retrievePageAssets(h.user.ID)
		}

	}
}

func (h *interactiveHandler) moveNextPage() {
	switch h.assetLoadPolicy {
	case "all":
		if !h.localPaginator.HasNext() {
			return
		}
		nextPageData := h.localPaginator.GetNextPageData()
		if len(h.currentData) != len(nextPageData) {
			h.currentData = make([]model.Asset, len(nextPageData))
		}
		for i, item := range nextPageData {
			h.currentData[i] = item.(model.Asset)
		}
	default:
		if h.remotePaginator.initialed && !h.remotePaginator.HasNext() {
			return
		}
		h.remotePaginator.initialed = true
		h.remotePaginator.offset += h.remotePaginator.limit
		switch h.remotePaginator.dataSource {
		case FromNode:
			h.currentData = h.remotePaginator.retrieveNodeAssets(h.user.ID, h.remotePaginator.currentNode.ID)
		default:
			h.currentData = h.remotePaginator.retrievePageAssets(h.user.ID)
		}
	}
}

func (h *interactiveHandler) getPageBasicInfo() (currentPage, pageSize, totalPage, totalCount int) {
	switch h.assetLoadPolicy {
	case "all":
		currentPage = h.localPaginator.CurrentPage()
		pageSize = h.localPaginator.PageSize()
		totalPage = h.localPaginator.TotalPage()
		totalCount = h.localPaginator.TotalCount()
	default:
		return h.getRemotePaginatorBasicInfo()
	}
	return
}

func (h *interactiveHandler) getRemotePaginatorBasicInfo() (currentPage, pageSize, totalPage, totalCount int) {
	currentOffset := h.remotePaginator.offset + len(h.currentSortedData)
	switch h.remotePaginator.limit {
	case 0:
		pageSize = len(h.currentSortedData)
		totalCount = pageSize
		totalPage = 1
		currentPage = 1
	default:
		pageSize = h.remotePaginator.limit
		totalCount = h.remotePaginator.Data.Total

		switch totalCount % pageSize {
		case 0:
			totalPage = totalCount / pageSize
		default:
			totalPage = (totalCount / pageSize) + 1
		}
		switch currentOffset % pageSize {
		case 0:
			currentPage = currentOffset / pageSize
		default:
			currentPage = (currentOffset / pageSize) + 1
		}
	}
	return
}

func (h *interactiveHandler) searchAssets(key string) []model.Asset {
	fmt.Println("searchAssets======>")

	switch h.assetLoadPolicy {
	case "all":
		h.currentData = h.searchAssetFromLocal(key)
	default:
		h.currentData = h.searchAssetFromRemote(key)
	}
	return h.currentData
}

func (h *interactiveHandler) searchOrProxy(key string) bool {
	if indexNum, err := strconv.Atoi(key); err == nil && len(h.currentSortedData) > 0 {
		if indexNum > 0 && indexNum <= len(h.currentSortedData) {
			assetSelect := h.currentSortedData[indexNum-1]
			h.ProxyAsset(assetSelect)
			return true
		}
	}
	if data := h.searchAssets(key); len(data) == 1 {
		h.ProxyAsset(data[0])
		return true
	}
	return false
}

func (h *interactiveHandler) searchAssetFromLocal(key string) []model.Asset {
	return searchFromLocalAssets(h.allAssets, key)
}

func (h *interactiveHandler) searchAssetFromRemote(key string) []model.Asset {
	switch h.remotePaginator.dataSource {
	case FromNode:
		return h.remotePaginator.retrieveNodeAssets(h.user.ID, h.remotePaginator.currentNode.ID)
	}
	return h.remotePaginator.retrievePageAssets(h.user.ID)
}

func (h *interactiveHandler) setPageSize() {
	pageSize := getPageSize(h.term)
	switch h.assetLoadPolicy {
	case "all":
		<-h.loadDataDone
		h.setLocalPageSize(pageSize)
	default:
		h.setRemotePageSize(pageSize)
	}
}

func (h *interactiveHandler) setLocalPageSize(pageSize int) {
	if pageSize > 0 {
		h.localPaginator.SetPageSize(pageSize)
	} else if pageSize == 0 && len(h.allAssets) > 0 {
		h.localPaginator.SetPageSize(len(h.allAssets))
	}
}

func (h *interactiveHandler) setRemotePageSize(pageSize int) {
	h.remotePaginator.SetPageSize(pageSize)
}

func (h *interactiveHandler) initPaginator() {
	pageSize := getPageSize(h.term)
	switch h.assetLoadPolicy {
	case "all":
		<-h.loadDataDone
		if pageSize == 0 {
			pageSize = len(h.allAssets)
		}
		pageData := make([]interface{}, len(h.allAssets))
		for i, v := range h.allAssets {
			pageData[i] = v
		}
		h.localPaginator = common.NewPagination(pageData, pageSize)
		return

	}
	h.remotePaginator = &remotePaginator{}
	h.remotePaginator.SetPageSize(pageSize)
}

func (h *interactiveHandler) searchNewNodeAssets(num int) {
	if num > len(h.nodes) || num == 0 {
		return
	}
	node := h.nodes[num-1]
	h.remotePaginator.dataSource = FromNode
	h.remotePaginator.offset = 0
	h.remotePaginator.limit = getPageSize(h.term)
	h.remotePaginator.search = ""
	h.remotePaginator.currentNode = node
	h.currentData = h.remotePaginator.retrieveNodeAssets(h.user.ID, node.ID)
}
