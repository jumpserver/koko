package handler

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/gliderlabs/ssh"
	"github.com/xlab/treeprint"

	"github.com/jumpserver/koko/pkg/common"
	"github.com/jumpserver/koko/pkg/config"
	"github.com/jumpserver/koko/pkg/i18n"
	"github.com/jumpserver/koko/pkg/logger"
	"github.com/jumpserver/koko/pkg/model"
	"github.com/jumpserver/koko/pkg/proxy"
	"github.com/jumpserver/koko/pkg/service"
	"github.com/jumpserver/koko/pkg/utils"
)

func SessionHandler(sess ssh.Session) {
	user, ok := sess.Context().Value(model.ContextKeyUser).(*model.User)
	if !ok || user.ID == "" {
		logger.Errorf("SSH User %s not found, exit.", sess.User())
		return
	}
	pty, _, ok := sess.Pty()
	if ok {
		handler := newInteractiveHandler(sess, user)
		logger.Infof("Request %s: User %s request pty %s", handler.sess.ID(), sess.User(), pty.Term)
		go handler.watchWinSizeChange()
		handler.Dispatch()
	} else {
		utils.IgnoreErrWriteString(sess, "No PTY requested.\n")
		return
	}
}

func newInteractiveHandler(sess ssh.Session, user *model.User) *interactiveHandler {
	wrapperSess := NewWrapperSession(sess)
	term := utils.NewTerminal(wrapperSess, "Opt> ")
	handler := &interactiveHandler{
		sess: wrapperSess,
		user: user,
		term: term,
	}
	handler.Initial()
	return handler
}

type interactiveHandler struct {
	sess         *WrapperSession
	user         *model.User
	term         *utils.Terminal
	winWatchChan chan bool

	assetSelect      *model.Asset
	systemUserSelect *model.SystemUser
	nodes            model.NodeList

	allAssets []model.Asset

	firstLoadDone   chan struct{}
	assetLoadPolicy string

	currentSortedData []model.Asset
	currentData       []model.Asset

	assetPaginator AssetPaginator

	dbPaginator   DatabasePaginator
	currentDBData []model.Database

	i18NT i18n.Language
}

func (h *interactiveHandler) Initial() {
	conf := config.GetConf()
	if conf.ClientAliveInterval > 0 {
		go h.keepSessionAlive(time.Duration(conf.ClientAliveInterval) * time.Second)
	}
	h.assetLoadPolicy = strings.ToLower(conf.AssetLoadPolicy)
	h.displayBanner()
	h.winWatchChan = make(chan bool, 5)
	h.firstLoadDone = make(chan struct{})
	go h.firstLoadData()
}

func (h *interactiveHandler) firstLoadData() {
	h.loadUserNodes("1")
	switch h.assetLoadPolicy {
	case "all":
		h.loadAllAssets()
	}
	close(h.firstLoadDone)
}

func (h *interactiveHandler) displayBanner() {
	displayBanner(h.sess, h.user.Name)
}

func (h *interactiveHandler) watchWinSizeChange() {
	sessChan := h.sess.WinCh()
	winChan := sessChan
	defer logger.Infof("Request %s: Windows change watch close", h.sess.Uuid)
	for {
		select {
		case <-h.sess.Sess.Context().Done():
			return
		case sig, ok := <-h.winWatchChan:
			if !ok {
				return
			}
			switch sig {
			case false:
				winChan = nil
			case true:
				winChan = sessChan
			}
		case win, ok := <-winChan:
			if !ok {
				return
			}
			logger.Debugf("Term window size change: %d*%d", win.Height, win.Width)
			_ = h.term.SetSize(win.Width, win.Height)
		}
	}
}

func (h *interactiveHandler) keepSessionAlive(keepAliveTime time.Duration) {
	t := time.NewTicker(keepAliveTime)
	defer t.Stop()
	for {
		select {
		case <-h.sess.Sess.Context().Done():
			return
		case <-t.C:
			_, err := h.sess.Sess.SendRequest("keepalive@openssh.com", true, nil)
			if err != nil {
				logger.Errorf("Request %s: Send user %s keepalive packet failed: %s",
					h.sess.Uuid, h.user.Name, err)
				continue
			}
			logger.Debugf("Request %s: Send user %s keepalive packet success", h.sess.Uuid, h.user.Name)
		}
	}
}

func (h *interactiveHandler) pauseWatchWinSize() {
	h.winWatchChan <- false
}

func (h *interactiveHandler) resumeWatchWinSize() {
	h.winWatchChan <- true
}

func (h *interactiveHandler) chooseSystemUser(asset model.Asset,
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
	selectUserTip := fmt.Sprintf(getI18nFromMap("SelectUserTip"), asset.Hostname, asset.IP)
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

func (h *interactiveHandler) refreshAssetsAndNodesData() {
	switch h.assetLoadPolicy {
	case "all":
		h.loadAllAssets()
	default:
		_ = service.ForceRefreshUserPemAssets(h.user.ID)
	}
	h.loadUserNodes("2")
	_, err := io.WriteString(h.term, getI18nFromMap("RefreshDone")+"\n\r")
	if err != nil {
		logger.Error("refresh Assets  Nodes err:", err)
	}
	h.assetPaginator = nil
	h.dbPaginator = nil
}

func (h *interactiveHandler) loadUserNodes(cachePolicy string) {
	h.nodes = service.GetUserNodes(h.user.ID, cachePolicy)
}

func (h *interactiveHandler) loadAllAssets() {
	h.allAssets = service.GetUserAllAssets(h.user.ID)
}

func (h *interactiveHandler) ProxyAsset(assetSelect model.Asset) {
	systemUsers := service.GetUserAssetSystemUsers(h.user.ID, assetSelect.ID)
	systemUserSelect, ok := h.chooseSystemUser(assetSelect, systemUsers)
	if !ok {
		return
	}
	h.systemUserSelect = &systemUserSelect
	h.assetSelect = &assetSelect
	h.Proxy(context.Background())
}

func (h *interactiveHandler) Proxy(ctx context.Context) {
	p := proxy.ProxyServer{
		UserConn:   h.sess,
		User:       h.user,
		Asset:      h.assetSelect,
		SystemUser: h.systemUserSelect,
	}
	h.pauseWatchWinSize()
	p.Proxy()
	h.resumeWatchWinSize()
	logger.Infof("Request %s: asset %s proxy end", h.sess.Uuid, h.assetSelect.Hostname)
}

func ConstructAssetNodeTree(assetNodes []model.Node) treeprint.Tree {
	model.SortAssetNodesByKey(assetNodes)
	var treeMap = map[string]treeprint.Tree{}
	tree := treeprint.New()
	for i := 0; i < len(assetNodes); i++ {
		r := strings.LastIndex(assetNodes[i].Key, ":")
		if r < 0 {
			subtree := tree.AddBranch(fmt.Sprintf("%s.%s(%s)",
				strconv.Itoa(i+1), assetNodes[i].Name,
				strconv.Itoa(assetNodes[i].AssetsAmount)))
			treeMap[assetNodes[i].Key] = subtree
			continue
		}
		if _, ok := treeMap[assetNodes[i].Key[:r]]; !ok {
			subtree := tree.AddBranch(fmt.Sprintf("%s.%s(%s)",
				strconv.Itoa(i+1), assetNodes[i].Name,
				strconv.Itoa(assetNodes[i].AssetsAmount)))
			treeMap[assetNodes[i].Key] = subtree
			continue
		}
		if subtree, ok := treeMap[assetNodes[i].Key[:r]]; ok {
			nodeTree := subtree.AddBranch(fmt.Sprintf("%s.%s(%s)",
				strconv.Itoa(i+1), assetNodes[i].Name,
				strconv.Itoa(assetNodes[i].AssetsAmount)))
			treeMap[assetNodes[i].Key] = nodeTree
		}
	}
	return tree
}

func isSubstring(sArray []string, substr string) bool {
	for _, s := range sArray {
		if strings.Contains(s, substr) {
			return true
		}
	}
	return false
}

func selectHighestPrioritySystemUsers(systemUsers []model.SystemUser) []model.SystemUser {
	length := len(systemUsers)
	if length == 0 {
		return systemUsers
	}
	var result = make([]model.SystemUser, 0)
	model.SortSystemUserByPriority(systemUsers)

	highestPriority := systemUsers[length-1].Priority

	result = append(result, systemUsers[length-1])
	for i := length - 2; i >= 0; i-- {
		if highestPriority == systemUsers[i].Priority {
			result = append(result, systemUsers[i])
		}
	}
	return result
}

func searchFromLocalAssets(assets model.AssetList, key string) []model.Asset {
	displayAssets := make([]model.Asset, 0, len(assets))
	key = strings.ToLower(key)
	for _, assetValue := range assets {
		contents := []string{strings.ToLower(assetValue.Hostname),
			strings.ToLower(assetValue.IP), strings.ToLower(assetValue.Comment)}
		if isSubstring(contents, key) {
			displayAssets = append(displayAssets, assetValue)
		}
	}
	return displayAssets
}

func getPageSize(term *utils.Terminal) int {
	var (
		pageSize  int
		minHeight = 8 // 分页显示的最小高度

	)
	_, height := term.GetSize()
	conf := config.GetConf()
	switch conf.AssetListPageSize {
	case "auto":
		pageSize = height - minHeight
	case "all":
		return 0
	default:
		if value, err := strconv.Atoi(conf.AssetListPageSize); err == nil {
			pageSize = value
		} else {
			pageSize = height - minHeight
		}
	}
	if pageSize <= 0 {
		pageSize = 1
	}
	return pageSize
}
