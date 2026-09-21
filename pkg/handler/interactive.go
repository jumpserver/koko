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

	"github.com/jumpserver-dev/sdk-go/model"
	"github.com/jumpserver-dev/sdk-go/service"

	"github.com/jumpserver/koko/pkg/common"
	"github.com/jumpserver/koko/pkg/config"
	"github.com/jumpserver/koko/pkg/i18n"
	"github.com/jumpserver/koko/pkg/logger"
	"github.com/jumpserver/koko/pkg/utils"
)

func NewInteractiveHandler(sess ssh.Session, user *model.User, jmsService *service.JMService,
	termConfig model.TerminalConfig) *InteractiveHandler {
	return newInteractiveHandler(NewWrapperSession(sess), user, jmsService, termConfig, nil, nil)
}

func newInteractiveHandler(sess *WrapperSession, user *model.User, jmsService *service.JMService,
	termConfig model.TerminalConfig, preferences *tuiPreferences, shutdown <-chan struct{}) *InteractiveHandler {
	language := getUserDefaultLangCode(user)
	if preferences != nil {
		language, _, _, _, _ = preferences.display(user.ID, language)
	}
	api := newLangAPIClient(jmsService, language)
	publicSetting, err := api.GetPublicSetting()
	if err != nil {
		logger.Errorf("Get public setting error: %s", err)
	}
	handler := &InteractiveHandler{
		sess: sess, user: user, term: term.NewTerminal(sess, "Opt> "), jmsService: api,
		terminalConf: &termConfig, i18nLang: language, publicSetting: &publicSetting,
		preferences: preferences, shutdown: shutdown,
	}
	handler.Initial()
	return handler
}

type InteractiveHandler struct {
	sess *WrapperSession
	user *model.User
	term *term.Terminal

	selectHandler   *UserSelectHandler
	nodes           model.NodeList
	typeNodes       []classicTypeNode
	favoriteNodes   []classicFavoriteNode
	assetLoadPolicy string
	wg              sync.WaitGroup
	jmsService      *service.JMService
	terminalConf    *model.TerminalConfig
	publicSetting   *model.PublicSetting
	i18nLang        string
	preferences     *tuiPreferences
	shutdown        <-chan struct{}
	manualPasswords tuiManualPasswordAttempts
}

func (h *InteractiveHandler) Initial() {
	conf := config.GetConf()
	h.assetLoadPolicy = strings.ToLower(conf.AssetLoadPolicy)
	h.displayHelp()
	hiddenFields := make(map[string]struct{}, len(conf.HiddenFields))
	for _, field := range conf.HiddenFields {
		hiddenFields[strings.ToLower(strings.TrimSpace(field))] = struct{}{}
	}
	h.selectHandler = &UserSelectHandler{
		user: h.user, h: h, pageInfo: &pageInfo{}, hiddenFields: hiddenFields,
	}
	if h.assetLoadPolicy == "all" {
		allAssets, err := h.jmsService.GetAllUserPermsAssets(h.user.ID)
		if err != nil {
			logger.Errorf("Get all user perms assets failed: %s", err)
		}
		h.selectHandler.SetAllLocalData(allAssets)
	}
	h.firstLoadData()
}

func (h *InteractiveHandler) GetPtySize() (int, int) {
	window := h.sess.Pty().Window
	width, height := window.Width, window.Height
	if width <= 0 {
		width = 80
	}
	if height <= 0 {
		height = 24
	}
	return min(500, width), min(200, height)
}

func (h *InteractiveHandler) resizeTerminal() {
	width, height := h.GetPtySize()
	_ = h.term.SetSize(width, height)
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
	h.displayAnnouncement(h.sess, h.publicSetting)
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
			_ = h.term.SetSize(min(500, max(1, win.Width)), min(200, max(1, win.Height)))
		}
	}
}

func (h *InteractiveHandler) watchSession(done <-chan struct{}) {
	interval := config.GetConf().ClientAliveInterval
	var keepAlive <-chan time.Time
	var ticker *time.Ticker
	if interval > 0 {
		ticker = time.NewTicker(time.Duration(interval) * time.Second)
		keepAlive = ticker.C
		defer ticker.Stop()
	}
	for {
		select {
		case <-h.shutdown:
			_ = h.sess.Sess.Close()
			return
		case <-done:
			return
		case <-h.sess.Context().Done():
			return
		case <-keepAlive:
			if _, err := h.sess.Sess.SendRequest("keepalive@openssh.com", true, nil); err != nil {
				logger.Errorf("Request %s: Send user %s keepalive packet failed: %s", h.sess.Uuid, h.user.Name, err)
			}
		}
	}
}

func (h *InteractiveHandler) tr(zh, en string) string {
	lang := i18n.NewLang(h.i18nLang)
	if lang == i18n.ZH {
		return zh
	}
	return lang.T(en)
}

func (h *InteractiveHandler) chooseAccount(permAccounts []model.PermAccount) (account model.PermAccount, ok, back bool) {
	lang := i18n.NewLang(h.i18nLang)
	switch len(permAccounts) {
	case 0:
		_, _ = io.WriteString(h.term, lang.T("No account found.")+"\n\r")
		return account, false, false
	case 1:
		return permAccounts[0], true, false
	}
	displayAccounts := model.PermAccountList(permAccounts)
	sort.Sort(displayAccounts)
	labels := []string{lang.T("Number"), lang.T("Name"), lang.T("Username")}
	fields := []string{"ID", "Name", "Username"}
	data := make([]map[string]string, len(displayAccounts))
	for i, account := range displayAccounts {
		data[i] = map[string]string{
			"ID": strconv.Itoa(i + 1), "Name": account.Name, "Username": account.Username,
		}
	}
	width, _ := h.GetPtySize()
	table := common.WrapperTable{
		Fields: fields, Labels: labels,
		FieldsSize: map[string][3]int{"ID": {0, 0, 5}, "Name": {0, 8, 0}, "Username": {0, 10, 0}},
		Data:       data, TotalSize: width, TruncPolicy: common.TruncMiddle,
	}
	table.Initial()
	h.resizeTerminal()
	h.term.SetPrompt("[Account]> ")
	accountHints := []string{fmt.Sprintf(lang.T("Current asset: %s"), h.selectHandler.selectedAsset.String())}
	accountHints = append(accountHints, compactClassicHintRows(width,
		fmt.Sprintf("[%s] %s", lang.T("Number"), lang.T("Select")),
		fmt.Sprintf("[b] %s", lang.T("Back")))...)
	for range 3 {
		utils.IgnoreErrWriteString(h.term, table.Display())
		utils.IgnoreErrWriteString(h.term, classicHintPanel(accountHints, width))
		line, err := h.term.ReadLine()
		if err != nil {
			logger.Errorf("select account err: %s", err)
			return account, false, false
		}
		line = strings.TrimSpace(line)
		switch strings.ToLower(line) {
		case "q", "b", "quit", "exit", "back":
			logger.Info("select account cancel")
			return account, false, true
		case "":
			continue
		}
		if num, err := strconv.Atoi(line); err == nil && num > 0 && num <= len(displayAccounts) {
			return displayAccounts[num-1], true, false
		}
	}
	utils.IgnoreErrWriteString(h.term, utils.WrapperWarn(lang.T("Select account exceed max retry times.")))
	utils.IgnoreErrWriteString(h.term, utils.CharNewLine)
	return account, false, false
}

func (h *InteractiveHandler) chooseAssetProtocol(protocols []string) (string, bool) {
	lang := i18n.NewLang(h.i18nLang)
	switch len(protocols) {
	case 0:
		_, _ = io.WriteString(h.term, lang.T("No protocol found.")+"\n\r")
		return "", false
	case 1:
		return protocols[0], true
	}
	data := make([]map[string]string, len(protocols))
	for i, protocol := range protocols {
		data[i] = map[string]string{"ID": strconv.Itoa(i + 1), "Protocol": protocol}
	}
	width, _ := h.GetPtySize()
	table := common.WrapperTable{
		Fields: []string{"ID", "Protocol"}, Labels: []string{lang.T("ID"), lang.T("Protocol")},
		FieldsSize: map[string][3]int{"ID": {0, 0, 5}, "Protocol": {0, 8, 0}},
		Data:       data, TotalSize: width, TruncPolicy: common.TruncMiddle,
	}
	table.Initial()
	h.resizeTerminal()
	h.term.SetPrompt("ID> ")
	hints := []string{fmt.Sprintf(lang.T("Current asset: %s"), h.selectHandler.selectedAsset.String())}
	hints = append(hints, compactClassicHintRows(width,
		fmt.Sprintf("[%s] %s", lang.T("Number"), lang.T("Select")),
		fmt.Sprintf("[b] %s", lang.T("Back")))...)
	for range 3 {
		utils.IgnoreErrWriteString(h.term, table.Display())
		utils.IgnoreErrWriteString(h.term, classicHintPanel(hints, width))
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
		case "":
			continue
		}
		if num, err := strconv.Atoi(line); err == nil && num > 0 && num <= len(protocols) {
			return protocols[num-1], true
		}
	}
	utils.IgnoreErrWriteString(h.term, utils.WrapperWarn(lang.T("Select protocol exceed max retry times.")))
	utils.IgnoreErrWriteString(h.term, utils.CharNewLine)
	time.Sleep(500 * time.Millisecond)
	return "", false
}

func (h *InteractiveHandler) refreshAuthorizationTreeCache() bool {
	h.wg.Wait()
	_, err := h.jmsService.GetUserPermsAssets(h.user.ID, model.PaginationParam{PageSize: 1, Refresh: true})
	if err != nil {
		logger.Errorf("Rebuild user authorization tree error: %s", err)
		utils.IgnoreErrWriteString(h.term, utils.WrapperWarn(i18n.NewLang(h.i18nLang).T("Core API failed")))
		return false
	}
	nodes, err := (tuiData{
		api: h.jmsService, userID: h.user.ID, lang: h.i18nLang,
	}).authorizationNodes()
	if err != nil {
		logger.Errorf("Refresh user authorization tree error: %s", err)
		utils.IgnoreErrWriteString(h.term, utils.WrapperWarn(i18n.NewLang(h.i18nLang).T("Core API failed")))
		return false
	}
	h.nodes = nodes
	if _, err := io.WriteString(h.term, i18n.NewLang(h.i18nLang).T("Refresh done")+"\n\r"); err != nil {
		logger.Error("refresh authorization tree err:", err)
	}
	return true
}

func (h *InteractiveHandler) loadUserNodes() {
	nodes, err := (tuiData{
		api: h.jmsService, userID: h.user.ID, lang: h.i18nLang,
	}).authorizationNodes()
	if err != nil {
		logger.Errorf("Get user nodes error: %s", err)
		return
	}
	h.nodes = nodes
}

func (h *InteractiveHandler) loadUserTypeNodes() bool {
	types, err := (tuiData{
		api: h.jmsService, userID: h.user.ID, lang: h.i18nLang,
	}).typeNodes()
	if err != nil {
		logger.Errorf("Get user type tree error: %s", err)
		return false
	}
	h.typeNodes = types
	return true
}

func (h *InteractiveHandler) loadUserFavoriteNodes() bool {
	favorites, err := (tuiData{
		api: h.jmsService, userID: h.user.ID, lang: h.i18nLang,
	}).favoriteNodes()
	if err != nil {
		logger.Errorf("Get user favorite tree error: %s", err)
		return false
	}
	h.favoriteNodes = favorites
	return true
}

func getPageSize(h *InteractiveHandler, termConf *model.TerminalConfig) int {
	_, height := h.GetPtySize()
	pageSize := height - 8
	switch termConf.AssetListPageSize {
	case "auto":
	case "all":
		return PAGESIZEALL
	default:
		if value, err := strconv.Atoi(termConf.AssetListPageSize); err == nil {
			pageSize = value
		}
	}
	return max(1, pageSize)
}

func ConstructNodeTree(assetNodes []model.Node) (treeprint.Tree, []model.Node) {
	model.SortNodesByKey(assetNodes)
	rootTree := treeprint.New()
	newNodes := make([]model.Node, 0, len(assetNodes))
	newNodes = constructDisplayTree(rootTree, convertToDisplayTrees(assetNodes), newNodes)
	return rootTree, newNodes
}

func constructNodeTreeRows(assetNodes []model.Node) ([]string, []model.Node) {
	model.SortNodesByKey(assetNodes)
	prefixes := make([]string, 0, len(assetNodes))
	ordered := make([]model.Node, 0, len(assetNodes))
	constructDisplayRows(convertToDisplayTrees(assetNodes), "", &prefixes, &ordered)
	return prefixes, ordered
}

func constructDisplayRows(nodes []*displayTree, prefix string, prefixes *[]string, ordered *[]model.Node) {
	for i, item := range nodes {
		last := i == len(nodes)-1
		edge := string(treeprint.EdgeTypeMid)
		childPrefix := prefix + string(treeprint.EdgeTypeLink) + strings.Repeat(" ", treeprint.IndentSize)
		if last {
			edge = string(treeprint.EdgeTypeEnd)
			childPrefix = prefix + strings.Repeat(" ", treeprint.IndentSize+1)
		}
		*prefixes = append(*prefixes, prefix+edge+" ")
		*ordered = append(*ordered, item.node)
		if len(item.subTrees) > 0 {
			sort.Sort(nodeTrees(item.subTrees))
			constructDisplayRows(item.subTrees, childPrefix, prefixes, ordered)
		}
	}
}

func constructDisplayTree(tree treeprint.Tree, rootNodes []*displayTree, newNodes []model.Node) []model.Node {
	for _, rootNode := range rootNodes {
		subTree := tree.AddBranch(fmt.Sprintf("%d.%s(%s)", len(newNodes)+1, rootNode.node.Name,
			strconv.Itoa(rootNode.node.AssetsAmount)))
		newNodes = append(newNodes, rootNode.node)
		if len(rootNode.subTrees) > 0 {
			sort.Sort(nodeTrees(rootNode.subTrees))
			newNodes = constructDisplayTree(subTree, rootNode.subTrees, newNodes)
		}
	}
	return newNodes
}

func convertToDisplayTrees(assetNodes []model.Node) []*displayTree {
	var rootNodeTrees []*displayTree
	nodeTreeMap := make(map[string]*displayTree)
	for i := range assetNodes {
		currentTree := displayTree{Key: assetNodes[i].Key, node: assetNodes[i]}
		separator := strings.LastIndex(assetNodes[i].Key, ":")
		if separator < 0 {
			rootNodeTrees = append(rootNodeTrees, &currentTree)
			nodeTreeMap[assetNodes[i].Key] = &currentTree
			continue
		}
		nodeTreeMap[assetNodes[i].Key] = &currentTree
		parentTree, ok := nodeTreeMap[assetNodes[i].Key[:separator]]
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

func (t *displayTree) AddSubNode(sub *displayTree) { t.subTrees = append(t.subTrees, sub) }

type nodeTrees []*displayTree

func (l nodeTrees) Len() int           { return len(l) }
func (l nodeTrees) Swap(i, j int)      { l[i], l[j] = l[j], l[i] }
func (l nodeTrees) Less(i, j int) bool { return l[i].node.Name < l[j].node.Name }
