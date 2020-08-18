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
	"github.com/jumpserver/koko/pkg/proxy"
	"github.com/jumpserver/koko/pkg/service"
	"github.com/jumpserver/koko/pkg/utils"
)

func (h *interactiveHandler) Dispatch() {
	defer logger.Infof("Request %s: User %s stop interactive", h.sess.ID(), h.user.Name)
        linechan := make(chan string)
        errchan := make(chan error)
	for {
		var line string
		var err error
		go h.readLine(linechan, errchan)
		
		select {
		case line = <-linechan:
		    fmt.Println(line)
		case err = <-errchan:
		    fmt.Println(err)
		    return
		case <-time.After(config.GetConf().ClientConTimeOut * time.Second):
		    fmt.Println("Timeout and exit")
		    return
		}
		
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
				if h.dbPaginator != nil {
					h.moveDBPrePage()
					break
				}
				if ok := h.searchOrProxy(line); ok {
					continue
				}
			case "d":
				h.assetPaginator = nil
				h.dbPaginator = h.getDatabasePaginator()
				h.currentDBData = h.dbPaginator.RetrievePageData(1)
			case "n":
				if h.assetPaginator != nil {
					h.moveNextPage()
					break
				}
				if h.dbPaginator != nil {
					h.moveDBNextPage()
					break
				}
				if ok := h.searchOrProxy(line); ok {
					continue
				}
			case "":
				if h.assetPaginator != nil {
					h.moveNextPage()
				} else if h.dbPaginator != nil {
					h.moveDBNextPage()
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
		if h.dbPaginator != nil {
			h.displayPageDatabase()
		}
		if h.assetPaginator != nil {
			h.displayPageAssets()
		}

	}
}

func (h *interactiveHandler) resetPaginator() {
	h.dbPaginator = nil
	h.currentDBData = nil
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
	utils.IgnoreErrWriteString(h.term, utils.WrapperString(actionTip, utils.Green))
	utils.IgnoreErrWriteString(h.term, utils.CharNewLine)
	utils.IgnoreErrWriteString(h.term, utils.WrapperString(searchHeader, utils.Green))
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
	if h.dbPaginator != nil {
		if indexNum, err := strconv.Atoi(key); err == nil && len(h.currentDBData) > 0 {
			if indexNum > 0 && indexNum <= len(h.currentDBData) {
				dbSelected := h.currentDBData[indexNum-1]
				h.ProxyDB(dbSelected)
				h.dbPaginator = nil
				h.currentDBData = nil
				return true
			}
		}
		if data := h.dbPaginator.SearchAsset(key); len(data) == 1 {
			h.ProxyDB(data[0])
			h.dbPaginator = nil
			h.currentDBData = nil
			return true
		} else {
			h.currentDBData = data
		}
		return false
	}
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
	h.currentDBData = nil
	h.dbPaginator = nil
	h.currentData = h.searchAssets(key)
}

func (h *interactiveHandler) searchAssetsAgain(key string) {
	if h.dbPaginator != nil {
		h.currentDBData = h.dbPaginator.SearchAgain(key)
		return
	}
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

func (h *interactiveHandler) getDatabasePaginator() DatabasePaginator {
	dbs := service.GetUserDatabases(h.user.ID)
	return NewLocalDatabasePaginator(dbs, getPageSize(h.term))
}

func (h *interactiveHandler) displayPageDatabase() {
	if len(h.currentDBData) == 0 {
		_, _ = h.term.Write([]byte(getI18nFromMap("NoDatabases") + "\n\r"))
		h.dbPaginator = nil
		return
	}
	Labels := []string{getI18nFromMap("ID"), getI18nFromMap("Name"),
		getI18nFromMap("IP"), getI18nFromMap("DBType"),
		getI18nFromMap("DBName"),getI18nFromMap("Comment")}
	fields := []string{"ID", "name", "IP", "DBType","DBName", "comment"}
	data := make([]map[string]string, len(h.currentDBData))
	for i, j := range h.currentDBData {
		row := make(map[string]string)
		row["ID"] = strconv.Itoa(i + 1)
		row["name"] = j.Name
		row["IP"] = j.Host
		row["DBType"] = j.DBType
		row["DBName"] = j.DBName

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

	currentPage := h.dbPaginator.CurrentPage()
	pageSize := h.dbPaginator.PageSize()
	totalPage := h.dbPaginator.TotalPage()
	totalCount := h.dbPaginator.TotalCount()

	caption := fmt.Sprintf(getI18nFromMap("AssetTableCaption"),
		currentPage, pageSize, totalPage, totalCount)

	caption = utils.WrapperString(caption, utils.Green)
	table := common.WrapperTable{
		Fields: fields,
		Labels: Labels,
		FieldsSize: map[string][3]int{
			"ID":      {0, 0, 5},
			"name":    {0, 8, 0},
			"IP":      {0, 15, 40},
			"DBType":  {0, 8, 0},
			"DBName":  {0, 8, 0},
			"comment": {0, 0, 0},
		},
		Data:        data,
		TotalSize:   w,
		Caption:     caption,
		TruncPolicy: common.TruncMiddle,
	}
	table.Initial()
	header := getI18nFromMap("All")
	keys := h.dbPaginator.SearchKeys()
	switch h.dbPaginator.Name() {
	case "local", "remote":
		if len(keys) != 0 {
			header = strings.Join(keys, " ")
		}
	default:
		header = fmt.Sprintf("%s %s", h.dbPaginator.Name(), strings.Join(keys, " "))
	}
	searchHeader := fmt.Sprintf(getI18nFromMap("SearchTip"), header)
	actionTip := fmt.Sprintf("%s %s", getI18nFromMap("DBLoginTip"), getI18nFromMap("PageActionTip"))

	_, _ = h.term.Write([]byte(utils.CharClear))
	_, _ = h.term.Write([]byte(table.Display()))
	utils.IgnoreErrWriteString(h.term, utils.WrapperString(actionTip, utils.Green))
	utils.IgnoreErrWriteString(h.term, utils.CharNewLine)
	utils.IgnoreErrWriteString(h.term, utils.WrapperString(searchHeader, utils.Green))
	utils.IgnoreErrWriteString(h.term, utils.CharNewLine)
}

func (h *interactiveHandler) moveDBPrePage() {
	if h.dbPaginator == nil || !h.dbPaginator.HasPrev() {
		return
	}
	h.dbPaginator.SetPageSize(getPageSize(h.term))
	prePage := h.dbPaginator.CurrentPage() - 1
	h.currentDBData = h.dbPaginator.RetrievePageData(prePage)
}

func (h *interactiveHandler) moveDBNextPage() {
	if h.dbPaginator == nil || !h.dbPaginator.HasNext() {
		return
	}
	h.dbPaginator.SetPageSize(getPageSize(h.term))
	prePage := h.dbPaginator.CurrentPage() + 1
	h.currentDBData = h.dbPaginator.RetrievePageData(prePage)
}

func (h *interactiveHandler) ProxyDB(dbSelect model.Database) {
	systemUsers := service.GetUserDatabaseSystemUsers(h.user.ID, dbSelect.ID)
	systemUserSelect, ok := h.chooseDBSystemUser(dbSelect, systemUsers)
	if !ok {
		return
	}
	p := proxy.DBProxyServer{
		UserConn:   h.sess,
		User:       h.user,
		Database:   &dbSelect,
		SystemUser: &systemUserSelect,
	}
	h.pauseWatchWinSize()
	p.Proxy()
	logger.Infof("Request %s: database %s proxy end", h.sess.Uuid, dbSelect.Name)
	h.resumeWatchWinSize()
}

func (h *interactiveHandler) chooseDBSystemUser(dbAsset model.Database,
	systemUsers []model.SystemUser) (systemUser model.SystemUser, ok bool) {

	length := len(systemUsers)
	switch length {
	case 0:
		return model.SystemUser{}, false
	case 1:
		return systemUsers[0], true
	default:
	}
	displaySystemUsers := selectHighestPrioritySystemUsers(systemUsers)
	if len(displaySystemUsers) == 1 {
		return displaySystemUsers[0], true
	}

	Labels := []string{getI18nFromMap("ID"), getI18nFromMap("Name"), getI18nFromMap("Username")}
	fields := []string{"ID", "Name", "Username"}

	data := make([]map[string]string, len(displaySystemUsers))
	for i, j := range displaySystemUsers {
		row := make(map[string]string)
		row["ID"] = strconv.Itoa(i + 1)
		row["Name"] = j.Name
		row["Username"] = j.Username
		data[i] = row
	}
	w, _ := h.term.GetSize()
	table := common.WrapperTable{
		Fields: fields,
		Labels: Labels,
		FieldsSize: map[string][3]int{
			"ID":       {0, 0, 5},
			"Name":     {0, 8, 0},
			"Username": {0, 10, 0},
		},
		Data:        data,
		TotalSize:   w,
		TruncPolicy: common.TruncMiddle,
	}
	table.Initial()

	h.term.SetPrompt("ID> ")
	defer h.term.SetPrompt("Opt> ")
	selectUserTip := fmt.Sprintf(getI18nFromMap("SelectUserTip"), dbAsset.Name, dbAsset.Host)
	for {
		utils.IgnoreErrWriteString(h.term, table.Display())
		utils.IgnoreErrWriteString(h.term, selectUserTip)
		utils.IgnoreErrWriteString(h.term, getI18nFromMap("BackTip"))
		utils.IgnoreErrWriteString(h.term, "\r\n")
		line, err := h.term.ReadLine()
		if err != nil {
			return
		}
		line = strings.TrimSpace(line)
		switch strings.ToLower(line) {
		case "q", "b", "quit", "exit", "back":
			return
		}
		if num, err := strconv.Atoi(line); err == nil {
			if num > 0 && num <= len(displaySystemUsers) {
				return displaySystemUsers[num-1], true
			}
		}
	}
}
