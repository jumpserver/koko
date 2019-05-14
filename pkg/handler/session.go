package handler

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"

	"github.com/gliderlabs/ssh"
	"github.com/olekukonko/tablewriter"
	"github.com/xlab/treeprint"
	"golang.org/x/crypto/ssh/terminal"

	"cocogo/pkg/cctx"
	"cocogo/pkg/logger"
	"cocogo/pkg/model"
	"cocogo/pkg/proxy"
	"cocogo/pkg/service"
	"cocogo/pkg/utils"
)

func SessionHandler(sess ssh.Session) {
	pty, _, ok := sess.Pty()
	if ok {
		ctx, cancel := cctx.NewContext(sess)
		defer cancel()
		handler := newInteractiveHandler(sess, ctx.User())
		handler.termHeight, handler.termWidth = pty.Window.Height, pty.Window.Width
		logger.Debugf("User Request pty: %s %s", sess.User(), pty.Term)
		handler.Dispatch(ctx)
	} else {
		utils.IgnoreErrWriteString(sess, "No PTY requested.\n")
		return
	}
}

func newInteractiveHandler(sess ssh.Session, user *model.User) *interactiveHandler {
	term := utils.NewTerminal(sess, "Opt> ")
	handler := &interactiveHandler{sess: sess, user: user, term: term}
	handler.Initial()
	return handler
}

type interactiveHandler struct {
	sess         ssh.Session
	user         *model.User
	term         *utils.Terminal
	winWatchChan chan bool

	assetSelect      *model.Asset
	systemUserSelect *model.SystemUser
	assets           model.AssetList
	searchResult     model.AssetList
	nodes            model.NodeList
	mu               *sync.RWMutex

	termWidth  int
	termHeight int
}

func (h *interactiveHandler) Initial() {
	h.displayBanner()
	h.loadUserAssets()
	h.loadUserAssetNodes()
	h.searchResult = h.assets
	h.winWatchChan = make(chan bool)
}

func (h *interactiveHandler) displayBanner() {
	displayBanner(h.sess, h.user.Name)
}

func (h *interactiveHandler) watchWinSizeChange() {
	_, sessChan, _ := h.sess.Pty()
	winChan := sessChan
	for {
		select {
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

func (h *interactiveHandler) pauseWatchWinSize() {
	h.winWatchChan <- false
}

func (h *interactiveHandler) resumeWatchWinSize() {
	h.winWatchChan <- true
}

func (h *interactiveHandler) Dispatch(ctx cctx.Context) {
	go h.watchWinSizeChange()

	for {
		line, err := h.term.ReadLine()

		if err != nil {
			if err != io.EOF {
				logger.Debug("user disconnected")
			} else {
				logger.Error("Read from user err: ", err)
			}
			break
		}
		line = strings.TrimSpace(line)
		switch len(line) {
		case 0, 1:
			switch strings.ToLower(line) {
			case "", "p":
				h.displayAssets(h.assets)
			case "g":
				h.displayNodes(h.nodes)
			case "h":
				h.displayBanner()
			case "r":
				h.refreshAssetsAndNodesData()
			case "q":
				logger.Info("exit session")
				return
			default:
				assets := h.searchAsset(line)
				h.searchResult = assets
				h.displayAssetsOrProxy(assets)
			}
		default:
			switch {
			case strings.Index(line, "/") == 0:
				searchWord := strings.TrimSpace(line[1:])
				assets := h.searchAsset(searchWord)
				h.searchResult = assets
				h.displayAssets(assets)
			case strings.Index(line, "g") == 0:
				searchWord := strings.TrimSpace(strings.TrimPrefix(line, "g"))
				if num, err := strconv.Atoi(searchWord); err == nil {
					if num >= 0 {
						assets := h.searchNodeAssets(num)
						h.displayAssets(assets)
						h.searchResult = assets
						continue
					}
				}
			default:
				assets := h.searchAsset(line)
				h.searchResult = assets
				h.displayAssetsOrProxy(assets)
			}
		}

	}
}

func (h *interactiveHandler) chooseSystemUser(systemUsers []model.SystemUser) model.SystemUser {
	length := len(systemUsers)
	switch length {
	case 0:
		return model.SystemUser{}
	case 1:
		return systemUsers[0]
	default:
	}
	displaySystemUsers := make([]model.SystemUser, 0)
	model.SortSystemUserByPriority(systemUsers)

	highestPriority := systemUsers[length-1].Priority

	displaySystemUsers = append(displaySystemUsers, systemUsers[length-1])
	for i := length - 2; i >= 0; i-- {
		if highestPriority == systemUsers[i].Priority {
			displaySystemUsers = append(displaySystemUsers, systemUsers[i])
		}
	}
	if len(displaySystemUsers) == 1 {
		return displaySystemUsers[0]
	}

	table := tablewriter.NewWriter(h.term)
	table.SetHeader([]string{"ID", "Username"})
	for i := 0; i < len(displaySystemUsers); i++ {
		table.Append([]string{strconv.Itoa(i + 1), displaySystemUsers[i].Username})
	}
	table.SetBorder(false)
	count := 0
	term := terminal.NewTerminal(h.sess, "num:")
	for count < 3 {
		table.Render()
		line, err := term.ReadLine()
		if err != nil {
			continue
		}
		if num, err := strconv.Atoi(line); err == nil {
			if num > 0 && num <= len(displaySystemUsers) {
				return displaySystemUsers[num-1]
			}
		}
		count++
	}
	return displaySystemUsers[0]
}

// 当资产的数量为1的时候，就进行代理转化
func (h *interactiveHandler) displayAssetsOrProxy(assets []model.Asset) {
	if len(assets) == 1 {
		systemUser := h.chooseSystemUser(assets[0].SystemUsers)
		h.assetSelect = &assets[0]
		h.systemUserSelect = &systemUser
		h.Proxy(context.TODO())
	} else {
		h.displayAssets(assets)
	}
}

func (h *interactiveHandler) displayAssets(assets model.AssetList) {
	if len(assets) == 0 {
		_, _ = io.WriteString(h.term, "\r\n No Assets\r\n\r")
	} else {
		pag := AssetPagination{
			term:        h.term,
			TotalNumber: len(assets),
			Data:        assets,
		}
		pag.Initial()
		selectOneAssets := pag.PaginationState()
		if len(selectOneAssets) == 1 {
			systemUser := h.chooseSystemUser(selectOneAssets[0].SystemUsers)
			h.assetSelect = &selectOneAssets[0]
			h.systemUserSelect = &systemUser
			h.Proxy(context.TODO())
		}
	}

}

func (h *interactiveHandler) displayNodes(nodes []model.Node) {
	tree := ConstructAssetNodeTree(nodes)
	tipHeaderMsg := "\r\nNode: [ ID.Name(Asset amount) ]"
	tipEndMsg := "Tips: Enter g+NodeID to display the host under the node, such as g1\r\n\r"

	_, err := io.WriteString(h.term, tipHeaderMsg)
	_, err = io.WriteString(h.term, tree.String())
	_, err = io.WriteString(h.term, tipEndMsg)
	if err != nil {
		logger.Info("displayAssetNodes err:", err)
	}

}

func (h *interactiveHandler) refreshAssetsAndNodesData() {
	_, err := io.WriteString(h.sess, "Refresh done\r\n")
	if err != nil {
		logger.Error("refresh Assets  Nodes err:", err)
	}
}

func (h *interactiveHandler) loadUserAssets() {
	h.assets = service.GetUserAssets(h.user.ID, "1")
}

func (h *interactiveHandler) loadUserAssetNodes() {
	h.nodes = service.GetUserNodes(h.user.ID, "1")
}

func (h *interactiveHandler) searchAsset(key string) (assets []model.Asset) {
	if indexNum, err := strconv.Atoi(key); err == nil {
		if indexNum > 0 && indexNum <= len(h.searchResult) {
			assets = []model.Asset{h.searchResult[indexNum-1]}
			return
		}
	}
	for _, assetValue := range h.searchResult {
		contents := []string{assetValue.Ip, assetValue.Hostname, assetValue.Comment}
		if isSubstring(contents, key) {
			assets = append(assets, assetValue)
		}
	}

	//	assetsData, _ := Cached.Load(h.user.ID)
	//	for _, assetValue := range assetsData.([]model.Asset) {
	//		if isSubstring([]string{assetValue.Ip, assetValue.Hostname, assetValue.Comment}, key) {
	//			assets = append(assets, assetValue)
	//		}
	//	}

	return assets
}

func (h *interactiveHandler) searchNodeAssets(num int) (assets []model.Asset) {
	if num > len(h.nodes) || num == 0 {
		return assets
	}
	return h.nodes[num-1].AssetsGranted

}

func (h *interactiveHandler) Proxy(ctx context.Context) {
	//h.assetSelect = &model.Asset{Hostname: "centos", Port: 22, Ip: "192.168.244.185", Protocol: "ssh"}
	//h.systemUserSelect = &model.SystemUser{Id: "5dd8b5a0-8cdb-4857-8629-faf811c525e1", Name: "web", Username: "root", Password: "redhat", Protocol: "telnet"}

	userConn := &proxy.UserSSHConnection{Session: h.sess}
	p := proxy.ProxyServer{
		UserConn:   userConn,
		User:       h.user,
		Asset:      h.assetSelect,
		SystemUser: h.systemUserSelect,
	}
	h.pauseWatchWinSize()
	p.Proxy()
	h.resumeWatchWinSize()
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
	var result = make([]model.SystemUser, 0)
	length := len(systemUsers)
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

//func (h *InteractiveHandler) JoinShareRoom(roomID string) {
//sshConn := userhome.NewSSHConn(h.sess)
//ctx, cancelFuc := context.WithCancel(h.sess.Context())
//
//_, winCh, _ := h.sess.Pty()
//go func() {
//	for {
//		select {
//		case <-ctx.Done():
//			return
//		case win, ok := <-winCh:
//			if !ok {
//				return
//			}
//			fmt.Println("join term change:", win)
//		}
//	}
//}()
//proxybak.Manager.JoinShareRoom(roomID, sshConn)
//logger.Info("exit room id:", roomID)
//cancelFuc()
//
//}

//	/*
//		1. 创建SSHConn，符合core.Conn接口
//		2. 创建一个session Home
//		3. 创建一个NodeConn，及相关的channel 可以是MemoryChannel 或者是redisChannel
//		4. session Home 与 proxy channel 交换数据
//	*/
//	ptyReq, winChan, _ := i.sess.Pty()
//	sshConn := userhome.NewSSHConn(i.sess)
//	serverAuth := transport.ServerAuth{
//		SessionID: uuid.NewV4().String(),
//		IP:        asset.Ip,
//		port:      asset.port,
//		Username:  systemUser.Username,
//		password:  systemUser.password,
//		PublicKey: parsePrivateKey(systemUser.privateKey)}
//
//	nodeConn, err := transport.NewNodeConn(i.sess.Context(), serverAuth, ptyReq, winChan)
//	if err != nil {
//		logger.Error(err)
//		return err
//	}
//	defer func() {
//		nodeConn.Close()
//		data := map[string]interface{}{
//			"id":          nodeConn.SessionID,
//			"user":        i.user.Username,
//			"asset":       asset.Hostname,
//			"org_id":      asset.OrgID,
//			"system_user": systemUser.Username,
//			"login_from":  "ST",
//			"remote_addr": i.sess.RemoteAddr().String(),
//			"is_finished": true,
//			"date_start":  nodeConn.StartTime.Format("2006-01-02 15:04:05 +0000"),
//			"date_end":    time.Now().UTC().Format("2006-01-02 15:04:05 +0000"),
//		}
//		postData, _ := json.Marshal(data)
//		appService.FinishSession(nodeConn.SessionID, postData)
//		appService.FinishReply(nodeConn.SessionID)
//	}()
//	data := map[string]interface{}{
//		"id":          nodeConn.SessionID,
//		"user":        i.user.Username,
//		"asset":       asset.Hostname,
//		"org_id":      asset.OrgID,
//		"system_user": systemUser.Username,
//		"login_from":  "ST",
//		"remote_addr": i.sess.RemoteAddr().String(),
//		"is_finished": false,
//		"date_start":  nodeConn.StartTime.Format("2006-01-02 15:04:05 +0000"),
//		"date_end":    nil,
//	}
//	postData, err := json.Marshal(data)
//
//	if !appService.CreateSession(postData) {
//		return err
//	}
//
//	memChan := transport.NewMemoryAgent(nodeConn)
//
//	Home := userhome.NewUserSessionHome(sshConn)
//	logger.Info("session Home ID: ", Home.SessionID())
//
//	err = proxy.Manager.Session(i.sess.Context(), Home, memChan)
//	if err != nil {
//		logger.Error(err)
//	}
//	return err
//}
//
