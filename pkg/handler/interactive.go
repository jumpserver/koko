package handler

import (
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gliderlabs/ssh"
	"github.com/xlab/treeprint"
	"golang.org/x/term"

	"github.com/jumpserver/koko/pkg/common"
	"github.com/jumpserver/koko/pkg/config"
	"github.com/jumpserver/koko/pkg/i18n"
	"github.com/jumpserver/koko/pkg/jms-sdk-go/model"
	"github.com/jumpserver/koko/pkg/jms-sdk-go/service"
	"github.com/jumpserver/koko/pkg/logger"
	"github.com/jumpserver/koko/pkg/utils"
)

func NewInteractiveHandler(sess ssh.Session, user *model.User, jmsService *service.JMService,
	termConfig model.TerminalConfig) *InteractiveHandler {
	wrapperSess := NewWrapperSession(sess)
	vt := term.NewTerminal(wrapperSess, "Opt> ")
	handler := &InteractiveHandler{
		sess:         wrapperSess,
		user:         user,
		term:         vt,
		jmsService:   jmsService,
		terminalConf: &termConfig,
	}
	handler.Initial()
	return handler
}

var (
	// 全局永久缓存 ssh 登录用户切换的语言
	userLangGlobalStore = sync.Map{}
)

func getUserDefaultLangCode(user *model.User) string {
	if langCode, ok := userLangGlobalStore.Load(user.ID); ok {
		return langCode.(string)
	}
	return config.GetConf().LanguageCode
}

func checkMaxIdleTime(maxIdleMinutes int, langCode string, user *model.User, sess ssh.Session, checkChan <-chan bool) {
	maxIdleTime := time.Duration(maxIdleMinutes) * time.Minute
	tick := time.NewTicker(maxIdleTime)
	defer tick.Stop()
	checkStatus := true
	for {
		select {
		case <-tick.C:
			if checkStatus {
				lang := i18n.NewLang(langCode)
				msg := fmt.Sprintf(lang.T("Connect idle more than %d minutes, disconnect"), maxIdleMinutes)
				_, _ = io.WriteString(sess, "\r\n"+msg+"\r\n")
				_ = sess.Close()
				logger.Infof("User %s input idle more than %d minutes", user.Name, maxIdleMinutes)
			}
		case <-sess.Context().Done():
			logger.Infof("Stop checking user %s input idle time", user.Name)
			return
		case checkStatus = <-checkChan:
			if !checkStatus {
				logger.Debugf("Stop checking user %s idle time if more than %d minutes", user.Name, maxIdleMinutes)
				continue
			}
			tick.Reset(maxIdleTime)
			logger.Debugf("Start checking user %s idle time if more than %d minutes", user.Name, maxIdleMinutes)
		}
	}
}

type InteractiveHandler struct {
	sess *WrapperSession
	user *model.User
	term *term.Terminal

	selectHandler *UserSelectHandler

	nodes model.NodeList

	assetLoadPolicy string

	wg sync.WaitGroup

	jmsService *service.JMService

	terminalConf *model.TerminalConfig

	i18nLang string
}

func (h *InteractiveHandler) Initial() {
	conf := config.GetConf()
	if conf.ClientAliveInterval > 0 {
		go h.keepSessionAlive(time.Duration(conf.ClientAliveInterval) * time.Second)
	}
	h.assetLoadPolicy = strings.ToLower(conf.AssetLoadPolicy)
	h.i18nLang = getUserDefaultLangCode(h.user)
	h.displayHelp()
	h.selectHandler = &UserSelectHandler{
		user:     h.user,
		h:        h,
		pageInfo: &pageInfo{},
	}
	switch h.assetLoadPolicy {
	case "all":
		allAssets, err := h.jmsService.GetAllUserPermsAssets(h.user.ID)
		if err != nil {
			logger.Errorf("Get all user perms assets failed: %s", err)
		}
		h.selectHandler.SetAllLocalData(allAssets)
	}
	h.firstLoadData()

}

func (h *InteractiveHandler) GetPtySize() (int, int) {
	// todo: 优化直接存储
	pty := h.sess.Pty()
	return pty.Window.Width, pty.Window.Height
}

func (h *InteractiveHandler) firstLoadData() {
	h.wg.Add(1)
	go func() {
		defer h.wg.Done()
		h.loadUserNodes()
	}()
}

func (h *InteractiveHandler) displayHelp() {
	h.term.SetPrompt("Opt> ")
	h.displayBanner(h.sess, h.user.Name, h.terminalConf)
}

func (h *InteractiveHandler) WatchWinSizeChange(winChan <-chan ssh.Window) {
	defer logger.Infof("Request %s: Windows change watch close", h.sess.Uuid)
	for {
		select {
		case <-h.sess.Sess.Context().Done():
			return
		case win, ok := <-winChan:
			if !ok {
				return
			}
			h.sess.SetWin(win)
			logger.Debugf("Term window size change: %d*%d", win.Height, win.Width)
			_ = h.term.SetSize(win.Width, win.Height)
		}
	}
}

func (h *InteractiveHandler) keepSessionAlive(keepAliveTime time.Duration) {
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

func (h *InteractiveHandler) chooseAccount(permAccounts []model.PermAccount) (model.PermAccount, bool) {
	lang := i18n.NewLang(h.i18nLang)
	length := len(permAccounts)
	switch length {
	case 0:
		warningInfo := lang.T("No account found.")
		_, _ = io.WriteString(h.term, warningInfo+"\n\r")
		return model.PermAccount{}, false
	case 1:
		return permAccounts[0], true
	default:
	}
	displaySystemUsers := permAccounts

	idLabel := lang.T("ID")
	nameLabel := lang.T("Name")
	usernameLabel := lang.T("Username")

	labels := []string{idLabel, nameLabel, usernameLabel}
	fields := []string{"ID", "Name", "Username"}

	data := make([]map[string]string, len(displaySystemUsers))
	for i, j := range displaySystemUsers {
		row := make(map[string]string)
		row["ID"] = strconv.Itoa(i + 1)
		row["Name"] = j.Name
		row["Username"] = j.Username
		data[i] = row
	}
	w, _ := h.GetPtySize()
	table := common.WrapperTable{
		Fields: fields,
		Labels: labels,
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
	userHandler := h.selectHandler

	h.term.SetPrompt("ID> ")
	selectTip := fmt.Sprintf(lang.T("Tips: Enter asset[%s] account ID"), userHandler.selectedAsset.String())
	backTip := lang.T("Back: B/b")
	for {
		utils.IgnoreErrWriteString(h.term, table.Display())
		utils.IgnoreErrWriteString(h.term, utils.WrapperString(selectTip, utils.Green))
		utils.IgnoreErrWriteString(h.term, utils.CharNewLine)
		utils.IgnoreErrWriteString(h.term, utils.WrapperString(backTip, utils.Green))
		utils.IgnoreErrWriteString(h.term, utils.CharNewLine)
		line, err := h.term.ReadLine()
		if err != nil {
			logger.Errorf("select account err: %s", err)
			return model.PermAccount{}, false
		}
		line = strings.TrimSpace(line)
		switch strings.ToLower(line) {
		case "q", "b", "quit", "exit", "back":
			logger.Info("select account cancel")
			return model.PermAccount{}, false
		}
		if num, err2 := strconv.Atoi(line); err2 == nil {
			if num > 0 && num <= len(displaySystemUsers) {
				return displaySystemUsers[num-1], true
			}
		} else {
			logger.Errorf("select account not right number %s", line)
			return model.PermAccount{}, false
		}
	}
}

func (h *InteractiveHandler) chooseAssetProtocol(protocols []string) (string, bool) {
	lang := i18n.NewLang(h.i18nLang)
	length := len(protocols)
	switch length {
	case 0:
		warningInfo := lang.T("No protocol found.")
		_, _ = io.WriteString(h.term, warningInfo+"\n\r")
		return "", false
	case 1:
		return protocols[0], true
	default:
	}
	displayProtocols := protocols

	idLabel := lang.T("ID")
	nameLabel := lang.T("Protocol")

	labels := []string{idLabel, nameLabel}
	fields := []string{"ID", "Protocol"}

	data := make([]map[string]string, len(displayProtocols))
	for i := range displayProtocols {
		row := make(map[string]string)
		row["ID"] = strconv.Itoa(i + 1)
		row["Protocol"] = displayProtocols[i]
		data[i] = row
	}
	w, _ := h.GetPtySize()
	table := common.WrapperTable{
		Fields: fields,
		Labels: labels,
		FieldsSize: map[string][3]int{
			"ID":       {0, 0, 5},
			"Protocol": {0, 8, 0},
		},
		Data:        data,
		TotalSize:   w,
		TruncPolicy: common.TruncMiddle,
	}
	table.Initial()

	h.term.SetPrompt("ID> ")
	selectTip := lang.T("Tips: Enter protocol ID")
	backTip := lang.T("Back: B/b")
	for {
		utils.IgnoreErrWriteString(h.term, table.Display())
		utils.IgnoreErrWriteString(h.term, utils.WrapperString(selectTip, utils.Green))
		utils.IgnoreErrWriteString(h.term, utils.CharNewLine)
		utils.IgnoreErrWriteString(h.term, utils.WrapperString(backTip, utils.Green))
		utils.IgnoreErrWriteString(h.term, utils.CharNewLine)
		line, err := h.term.ReadLine()
		if err != nil {
			logger.Errorf("select protocol err: %s", err)
			return "", false
		}
		line = strings.TrimSpace(line)
		switch strings.ToLower(line) {
		case "q", "b", "quit", "exit", "back":
			logger.Info("select account cancel")
			return "", false
		}
		if num, err2 := strconv.Atoi(line); err2 == nil {
			if num > 0 && num <= len(displayProtocols) {
				return displayProtocols[num-1], true
			}
		} else {
			logger.Errorf("select protocol not right number %s", line)
			return "", false
		}
	}
}

func (h *InteractiveHandler) refreshAssetsAndNodesData() {
	h.wg.Add(2)
	go func() {
		defer h.wg.Done()
		allAssets, err := h.jmsService.RefreshUserAllPermsAssets(h.user.ID)
		if err != nil {
			logger.Errorf("Refresh user all perms assets error: %s", err)
			return
		}
		if h.assetLoadPolicy == "all" {
			h.selectHandler.SetAllLocalData(allAssets)
		}
	}()
	go func() {
		defer h.wg.Done()
		nodes, err := h.jmsService.RefreshUserNodes(h.user.ID)
		if err != nil {
			logger.Errorf("Refresh user nodes error: %s", err)
			return
		}
		h.nodes = nodes
		tConfig, err := h.jmsService.GetTerminalConfig()
		if err != nil {
			logger.Errorf("Refresh user terminal config error: %s", err)
			return
		}
		h.terminalConf = &tConfig
	}()
	h.wg.Wait()
	lang := i18n.NewLang(h.i18nLang)
	_, err := io.WriteString(h.term, lang.T("Refresh done")+"\n\r")
	if err != nil {
		logger.Error("refresh Assets Nodes err:", err)
	}
}

func (h *InteractiveHandler) loadUserNodes() {
	nodes, err := h.jmsService.GetUserNodes(h.user.ID)
	if err != nil {
		logger.Errorf("Get user nodes error: %s", err)
		return
	}
	h.nodes = nodes
}

func getPageSize(h *InteractiveHandler, termConf *model.TerminalConfig) int {
	var (
		pageSize  int
		minHeight = 8 // 分页显示的最小高度

	)
	_, height := h.GetPtySize()

	AssetListPageSize := termConf.AssetListPageSize
	switch AssetListPageSize {
	case "auto":
		pageSize = height - minHeight
	case "all":
		return PAGESIZEALL
	default:
		if value, err := strconv.Atoi(AssetListPageSize); err == nil {
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

func ConstructNodeTree(assetNodes []model.Node) treeprint.Tree {
	model.SortNodesByKey(assetNodes)
	keyIndexMap := make(map[string]int)
	for index := range assetNodes {
		keyIndexMap[assetNodes[index].Key] = index
	}
	rootTree := treeprint.New()
	constructDisplayTree(rootTree, convertToDisplayTrees(assetNodes), keyIndexMap)
	return rootTree
}

func constructDisplayTree(tree treeprint.Tree, rootNodes []*displayTree, keyMap map[string]int) {
	for i := 0; i < len(rootNodes); i++ {
		subTree := tree.AddBranch(fmt.Sprintf("%d.%s(%s)",
			keyMap[rootNodes[i].Key]+1, rootNodes[i].node.Name,
			strconv.Itoa(rootNodes[i].node.AssetsAmount)))
		if len(rootNodes[i].subTrees) > 0 {
			sort.Sort(nodeTrees(rootNodes[i].subTrees))
			constructDisplayTree(subTree, rootNodes[i].subTrees, keyMap)
		}
	}
}

func convertToDisplayTrees(assetNodes []model.Node) []*displayTree {
	var rootNodeTrees []*displayTree
	nodeTreeMap := make(map[string]*displayTree)
	for i := 0; i < len(assetNodes); i++ {
		currentTree := displayTree{
			Key:  assetNodes[i].Key,
			node: assetNodes[i],
		}
		r := strings.LastIndex(assetNodes[i].Key, ":")
		if r < 0 {
			rootNodeTrees = append(rootNodeTrees, &currentTree)
			nodeTreeMap[assetNodes[i].Key] = &currentTree
			continue
		}
		nodeTreeMap[assetNodes[i].Key] = &currentTree
		parentKey := assetNodes[i].Key[:r]

		parentTree, ok := nodeTreeMap[parentKey]
		if !ok {
			rootNodeTrees = append(rootNodeTrees, &currentTree)
			continue
		}
		parentTree.AddSubNode(&currentTree)
	}
	return rootNodeTrees
}

type displayTree struct {
	Key      string
	node     model.Node
	subTrees []*displayTree
}

func (t *displayTree) AddSubNode(sub *displayTree) {
	t.subTrees = append(t.subTrees, sub)
}

type nodeTrees []*displayTree

func (l nodeTrees) Len() int {
	return len(l)
}

func (l nodeTrees) Swap(i, j int) {
	l[i], l[j] = l[j], l[i]
}

func (l nodeTrees) Less(i, j int) bool {
	return l[i].node.Name < l[j].node.Name
}
