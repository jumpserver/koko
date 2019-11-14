package handler

import (
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/jumpserver/koko/pkg/common"
	"github.com/jumpserver/koko/pkg/config"
	"github.com/jumpserver/koko/pkg/logger"
	"github.com/jumpserver/koko/pkg/model"
	"github.com/jumpserver/koko/pkg/utils"
)

func (h *interactiveHandler) NewDispatch() {
	defer logger.Infof("Request %s: User %s stop interactive", h.sess.ID(), h.user.Name)
	for {
		line, err := h.term.ReadLine()
		if err != nil {
			logger.Debugf("User %s close connect", h.user.Name)
			break
		}
		line = strings.TrimSpace(line)
		switch len(line) {
		case 0, 1:
			switch strings.ToLower(line) {
			case "p":
				h.resetPaginator()
			case "b":
				if h.assetPaginator != nil {
					h.movePrePage()
					break
				}
				if ok := h.searchOrProxy(line); ok {
					continue
				}
			case "n":
				if h.assetPaginator != nil {
					h.moveNextPage()
					break
				}
				if ok := h.searchOrProxy(line); ok {
					continue
				}
			case "":
				if h.assetPaginator != nil {
					h.moveNextPage()
				} else {
					h.resetPaginator()
				}
			case "g":
				h.displayNodeTree()
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
				if strings.Index(line[1:], "/") == 0 {
					line = strings.TrimSpace(line[2:])
					h.searchAssetsAgain(line)
					break
				}
				line = strings.TrimSpace(line[1:])
				h.searchAssetAndDisplay(line)
			case strings.Index(line, "g") == 0:
				searchWord := strings.TrimSpace(strings.TrimPrefix(line, "g"))
				if num, err := strconv.Atoi(searchWord); err == nil {
					if num >= 0 {
						h.searchNewNodeAssets(num)
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

func (h *interactiveHandler) resetPaginator() {
	h.assetPaginator = h.getAssetPaginator()
	h.currentData = h.assetPaginator.RetrievePageData(1)
}

func (h *interactiveHandler) displayPageAssets() {
	if len(h.currentData) == 0 {
		_, _ = h.term.Write([]byte(getI18nFromMap("NoAssets") + "\n\r"))
		h.assetPaginator = nil
		h.currentSortedData = nil
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

	currentPage := h.assetPaginator.CurrentPage()
	pageSize := h.assetPaginator.PageSize()
	totalPage := h.assetPaginator.TotalPage()
	totalCount := h.assetPaginator.TotalCount()

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
	header := getI18nFromMap("All")
	keys := h.assetPaginator.SearchKeys()
	switch h.assetPaginator.Name() {
	case "local", "remote":
		if len(keys) != 0 {
			header = strings.Join(keys, " ")
		}
	default:
		header = fmt.Sprintf("%s %s", h.assetPaginator.Name(), strings.Join(keys, " "))
	}
	searchHeader := fmt.Sprintf(getI18nFromMap("SearchTip"), header)
	actionTip := fmt.Sprintf("%s %s", getI18nFromMap("LoginTip"), getI18nFromMap("PageActionTip"))

	_, _ = h.term.Write([]byte(utils.CharClear))
	_, _ = h.term.Write([]byte(table.Display()))
	utils.IgnoreErrWriteString(h.term, actionTip)
	utils.IgnoreErrWriteString(h.term, utils.CharNewLine)
	utils.IgnoreErrWriteString(h.term, utils.WrapperTitle(searchHeader))
	utils.IgnoreErrWriteString(h.term, utils.CharNewLine)
}

func (h *interactiveHandler) movePrePage() {
	if h.assetPaginator == nil || !h.assetPaginator.HasPrev() {
		return
	}
	h.assetPaginator.SetPageSize(getPageSize(h.term))
	prePage := h.assetPaginator.CurrentPage() - 1
	h.currentData = h.assetPaginator.RetrievePageData(prePage)
}

func (h *interactiveHandler) moveNextPage() {
	if h.assetPaginator == nil || !h.assetPaginator.HasNext() {
		return
	}
	h.assetPaginator.SetPageSize(getPageSize(h.term))
	nextPage := h.assetPaginator.CurrentPage() + 1
	h.currentData = h.assetPaginator.RetrievePageData(nextPage)
}

func (h *interactiveHandler) searchAssets(key string) []model.Asset {
	if _, ok := h.assetPaginator.(*nodeAssetsPaginator); ok {
		h.assetPaginator = nil
	}
	if h.assetPaginator == nil {
		h.assetPaginator = h.getAssetPaginator()
	}
	return h.assetPaginator.SearchAsset(key)

}

func (h *interactiveHandler) searchOrProxy(key string) bool {
	if indexNum, err := strconv.Atoi(key); err == nil && len(h.currentSortedData) > 0 {
		if indexNum > 0 && indexNum <= len(h.currentSortedData) {
			assetSelect := h.currentSortedData[indexNum-1]
			h.ProxyAsset(assetSelect)
			h.assetPaginator = nil
			h.currentSortedData = nil
			return true
		}
	}
	if data := h.searchAssets(key); len(data) == 1 {
		h.ProxyAsset(data[0])
		h.assetPaginator = nil
		h.currentSortedData = nil
		return true
	} else {
		h.currentData = data
	}
	return false
}

func (h *interactiveHandler) searchAssetAndDisplay(key string) {
	h.currentData = h.searchAssets(key)
}
func (h *interactiveHandler) searchAssetsAgain(key string) {
	if h.assetPaginator == nil {
		h.assetPaginator = h.getAssetPaginator()
		h.currentData = h.assetPaginator.SearchAsset(key)
		return
	}
	h.currentData = h.assetPaginator.SearchAgain(key)
}

func (h *interactiveHandler) displayNodeTree() {
	<-h.firstLoadDone
	tree := ConstructAssetNodeTree(h.nodes)
	_, _ = io.WriteString(h.term, "\n\r"+getI18nFromMap("NodeHeaderTip"))
	_, _ = io.WriteString(h.term, tree.String())
	_, err := io.WriteString(h.term, getI18nFromMap("NodeEndTip")+"\n\r")
	if err != nil {
		logger.Info("displayAssetNodes err:", err)
	}
}

func (h *interactiveHandler) searchNewNodeAssets(num int) {
	<-h.firstLoadDone

	if num > len(h.nodes) || num == 0 {
		h.currentData = nil
		return
	}
	node := h.nodes[num-1]
	h.assetPaginator = h.getNodeAssetPaginator(node)
	h.currentData = h.assetPaginator.RetrievePageData(1)
}

func (h *interactiveHandler) getAssetPaginator() AssetPaginator {
	switch h.assetLoadPolicy {
	case "all":
		<-h.firstLoadDone
		return NewLocalAssetPaginator(h.allAssets, getPageSize(h.term))
	default:
	}
	return NewRemoteAssetPaginator(*h.user, getPageSize(h.term))
}

func (h *interactiveHandler) getNodeAssetPaginator(node model.Node) AssetPaginator {
	return NewNodeAssetPaginator(*h.user, node, getPageSize(h.term))
}
