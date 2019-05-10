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
		logger.Debugf("User Request pty: %s %s", sess.User(), pty.Term)
		handler.Dispatch(ctx)
	} else {
		utils.IgnoreErrWriteString(sess, "No PTY requested.\n")
		return
	}
}

func newInteractiveHandler(sess ssh.Session, user *model.User) *interactiveHandler {
	term := terminal.NewTerminal(sess, "Opt> ")
	handler := &interactiveHandler{sess: sess, user: user, term: term}
	handler.Initial()
	return handler
}

type interactiveHandler struct {
	sess ssh.Session
	user *model.User
	term *terminal.Terminal

	assetSelect      *model.Asset
	systemUserSelect *model.SystemUser
	assets           model.AssetList
	searchResult     model.AssetList
	nodes            model.NodeList
	mu               *sync.RWMutex
}

func (h *interactiveHandler) Initial() {
	h.displayBanner()
	h.loadUserAssets()
	h.loadUserAssetNodes()
}

func (h *interactiveHandler) displayBanner() {
	displayBanner(h.sess, h.user.Name)
}

func (h *interactiveHandler) watchWinSizeChange(winCh <-chan ssh.Window, done <-chan struct{}) {
	for {
		select {
		case <-done:
			logger.Debug("Interactive handler watch win size done")
			return
		case win, ok := <-winCh:
			if !ok {
				return
			}
			logger.Debugf("Term window size change: %d*%d", win.Height, win.Width)
			_ = h.term.SetSize(win.Width, win.Height)
		}
	}
}

func (h *interactiveHandler) Dispatch(ctx cctx.Context) {
	_, winCh, _ := h.sess.Pty()
	for {
		doneChan := make(chan struct{})
		go h.watchWinSizeChange(winCh, doneChan)
		line, err := h.term.ReadLine()
		close(doneChan)

		if err != nil {
			if err != io.EOF {
				logger.Debug("user disconnected")
			} else {
				logger.Error("Read from user err: ", err)
			}
			break
		}

		switch len(line) {
		case 0, 1:
			switch strings.ToLower(line) {
			case "", "p":
				h.Proxy(ctx)
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
	table := tablewriter.NewWriter(h.sess)
	table.SetHeader([]string{"ID", "Username"})
	for i := 0; i < len(systemUsers); i++ {
		table.Append([]string{strconv.Itoa(i + 1), systemUsers[i].Username})
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
			if num > 0 && num <= len(systemUsers) {
				return systemUsers[num-1]
			}
		}
		count++
	}
	return systemUsers[0]
}

// 当资产的数量为1的时候，就进行代理转化
func (h *interactiveHandler) displayAssetsOrProxy(assets []model.Asset) {
	//if len(assets) == 1 {
	//	var systemUser model.SystemUser
	//	switch len(assets[0].SystemUsers) {
	//	case 0:
	//		// 有授权的资产，但是资产用户信息，无法登陆
	//		h.displayAssets(assets)
	//		return
	//	case 1:
	//		systemUser = assets[0].SystemUsers[0]
	//	default:
	//		systemUser = h.chooseSystemUser(assets[0].SystemUsers)
	//	}
	//
	//	authInfo, err := model.GetSystemUserAssetAuthInfo(systemUser.ID, assets[0].ID)
	//	if err != nil {
	//		return
	//	}
	//	if ok := service.ValidateUserAssetPermission(h.user.ID, systemUser.ID, assets[0].ID); !ok {
	//		// 检查user 是否对该资产有权限
	//		return
	//	}
	//
	//	err = h.Proxy(assets[0], authInfo)
	//	if err != nil {
	//		logger.Info(err)
	//	}
	//	return
	//} else {
	//	h.displayAssets(assets)
	//}
}

func (h *interactiveHandler) displayAssets(assets model.AssetList) {
	if len(assets) == 0 {
		_, _ = io.WriteString(h.sess, "\r\n No Assets\r\n\r")
	} else {
		table := tablewriter.NewWriter(h.sess)
		table.SetHeader([]string{"ID", "Hostname", "IP", "LoginAs", "Comment"})
		for index, assetItem := range assets {
			sysUserArray := make([]string, len(assetItem.SystemUsers))
			for index, sysUser := range assetItem.SystemUsers {
				sysUserArray[index] = sysUser.Name
			}
			sysUsers := "[" + strings.Join(sysUserArray, " ") + "]"
			table.Append([]string{strconv.Itoa(index + 1), assetItem.Hostname, assetItem.Ip, sysUsers, assetItem.Comment})
		}

		table.SetBorder(false)
		table.Render()
	}

}

func (h *interactiveHandler) displayNodes(nodes []model.Node) {
	tree := ConstructAssetNodeTree(nodes)
	tipHeaderMsg := "\r\nNode: [ ID.Name(Asset amount) ]"
	tipEndMsg := "Tips: Enter g+NodeID to display the host under the node, such as g1\r\n\r"

	_, err := io.WriteString(h.sess, tipHeaderMsg)
	_, err = io.WriteString(h.sess, tree.String())
	_, err = io.WriteString(h.sess, tipEndMsg)
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

func (h *interactiveHandler) changeLanguage() {

}

func (h *interactiveHandler) JoinShareRoom(roomID string) {
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
	logger.Info("exit room id:", roomID)
	//cancelFuc()

}

func (h *interactiveHandler) searchAsset(key string) (assets []model.Asset) {
	//if indexNum, err := strconv.Atoi(key); err == nil {
	//	if indexNum > 0 && indexNum <= len(h.searchResult) {
	//		return []model.Asset{h.searchResult[indexNum-1]}
	//	}
	//}
	//
	//if assetsData, ok := h.assetData.Load(AssetsMapKey); ok {
	//	for _, assetValue := range assetsData.([]model.Asset) {
	//		if isSubstring([]string{assetValue.Ip, assetValue.Hostname, assetValue.Comment}, key) {
	//			assets = append(assets, assetValue)
	//		}
	//	}
	//} else {
	//	assetsData, _ := Cached.Load(h.user.ID)
	//	for _, assetValue := range assetsData.([]model.Asset) {
	//		if isSubstring([]string{assetValue.Ip, assetValue.Hostname, assetValue.Comment}, key) {
	//			assets = append(assets, assetValue)
	//		}
	//	}
	//}

	return assets
}

func (h *interactiveHandler) searchNodeAssets(num int) (assets []model.Asset) {
	//var assetNodesData []model.Node
	//if assetNodes, ok := h.assetData.Load(AssetNodesMapKey); ok {
	//	assetNodesData = assetNodes.([]model.Node)
	//	if num > len(assetNodesData) || num == 0 {
	//		return assets
	//	}
	//	return assetNodesData[num-1].AssetsGranted
	//}
	return assets

}

func (h *interactiveHandler) Proxy(ctx context.Context) {
	h.assetSelect = &model.Asset{Hostname: "centos", Port: 22, Ip: "192.168.244.185", Protocol: "ssh"}
	h.systemUserSelect = &model.SystemUser{Id: "5dd8b5a0-8cdb-4857-8629-faf811c525e1", Name: "web", Username: "root", Password: "redhat", Protocol: "telnet"}

	userConn := &proxy.UserSSHConnection{Session: h.sess}
	p := proxy.ProxyServer{
		UserConn:   userConn,
		User:       h.user,
		Asset:      h.assetSelect,
		SystemUser: h.systemUserSelect,
	}
	p.Proxy()
}

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
//func isSubstring(sArray []string, substr string) bool {
//	for _, s := range sArray {
//		if strings.Contains(s, substr) {
//			return true
//		}
//	}
//	return false
//}
//
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
