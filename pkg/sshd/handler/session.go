package handler

import (
	//"context"
	//"strconv"

	"cocogo/pkg/model"
	"cocogo/pkg/proxy"
	"cocogo/pkg/sdk"
	"cocogo/pkg/service"
	"cocogo/pkg/userhome"
	"context"
	"fmt"
	"github.com/olekukonko/tablewriter"
	"github.com/xlab/treeprint"

	//"encoding/json"
	//"fmt"
	"io"
	"strconv"
	"strings"

	//"strings"

	//"strconv"
	//"strings"
	"sync"
	//"time"

	"github.com/gliderlabs/ssh"
	//"github.com/olekukonko/tablewriter"
	//"github.com/satori/go.uuid"
	//"github.com/xlab/treeprint"
	"golang.org/x/crypto/ssh/terminal"

	"cocogo/pkg/logger"
	//"cocogo/pkg/proxy"
	//"cocogo/pkg/service"
	//"cocogo/pkg/transport"
	//"cocogo/pkg/userhome"
)

type InteractiveHandler struct {
	sess         ssh.Session
	term         *terminal.Terminal
	user         sdk.User
	assets       model.AssetList
	searchResult model.AssetList
	nodes        model.NodeList
	onceLoad     sync.Once
	sync.RWMutex
}

func (i *InteractiveHandler) displayBanner() {
	displayBanner(i.sess, i.user.Name)
}

func (i *InteractiveHandler) preDispatch() {
	i.displayBanner()
	i.onceLoad.Do(func() {
		i.loadUserAssets()
		i.loadUserAssetNodes()
	})
}

func (i *InteractiveHandler) watchWinSizeChange(winCh <-chan ssh.Window) {
	ctx, cancelFunc := context.WithCancel(i.sess.Context())
	defer cancelFunc()
	for {
		select {
		case <-ctx.Done():
			logger.Info("ctx done")
			return
		case win, ok := <-winCh:
			if !ok {
				return
			}
			logger.Info("Term change:", win)
			_ = i.term.SetSize(win.Width, win.Height)
		}
	}
}

func (i *InteractiveHandler) Dispatch() {
	i.preDispatch()
	_, winCh, _ := i.sess.Pty()
	for {
		go i.watchWinSizeChange(winCh)

		line, err := i.term.ReadLine()
		if err != nil {
			logger.Error("ReadLine done", err)
			break
		}

		switch len(line) {
		case 0, 1:
			switch strings.ToLower(line) {
			case "", "p":
				i.displayAssets(i.assets)
			case "g":
				i.displayNodes(i.nodes)
			case "s":
				i.changeLanguage()
			case "h":
				i.displayBanner()
			case "r":
				i.refreshAssetsAndNodesData()
			case "q":
				logger.Info("exit session")
				return
			default:
				assets := i.searchAsset(line)
				i.searchResult = assets
				i.displayAssetsOrProxy(assets)
			}
		default:
			switch {
			case strings.Index(line, "/") == 0:
				searchWord := strings.TrimSpace(line[1:])
				assets := i.searchAsset(searchWord)
				i.searchResult = assets
				i.displayAssets(assets)
			case strings.Index(line, "g") == 0:
				searchWord := strings.TrimSpace(strings.TrimPrefix(line, "g"))
				if num, err := strconv.Atoi(searchWord); err == nil {
					if num >= 0 {
						assets := i.searchNodeAssets(num)
						i.displayAssets(assets)
						i.searchResult = assets
						continue
					}
				}
			}
		}
	}
}

func (i *InteractiveHandler) chooseSystemUser(systemUsers []sdk.SystemUser) sdk.SystemUser {
	table := tablewriter.NewWriter(i.sess)
	table.SetHeader([]string{"ID", "UserName"})
	for i := 0; i < len(systemUsers); i++ {
		table.Append([]string{strconv.Itoa(i + 1), systemUsers[i].UserName})
	}
	table.SetBorder(false)
	count := 0
	term := terminal.NewTerminal(i.sess, "num:")
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
func (i *InteractiveHandler) displayAssetsOrProxy(assets []sdk.Asset) {
	//if len(assets) == 1 {
	//	var systemUser sdk.SystemUser
	//	switch len(assets[0].SystemUsers) {
	//	case 0:
	//		// 有授权的资产，但是资产用户信息，无法登陆
	//		i.displayAssets(assets)
	//		return
	//	case 1:
	//		systemUser = assets[0].SystemUsers[0]
	//	default:
	//		systemUser = i.chooseSystemUser(assets[0].SystemUsers)
	//	}
	//
	//	authInfo, err := sdk.GetSystemUserAssetAuthInfo(systemUser.Id, assets[0].Id)
	//	if err != nil {
	//		return
	//	}
	//	if ok := service.ValidateUserAssetPermission(i.user.Id, systemUser.Id, assets[0].Id); !ok {
	//		// 检查user 是否对该资产有权限
	//		return
	//	}
	//
	//	err = i.Proxy(assets[0], authInfo)
	//	if err != nil {
	//		logger.Info(err)
	//	}
	//	return
	//} else {
	//	i.displayAssets(assets)
	//}
}

func (i *InteractiveHandler) displayAssets(assets model.AssetList) {
	if len(assets) == 0 {
		_, _ = io.WriteString(i.sess, "\r\n No Assets\r\n\r")
	} else {
		table := tablewriter.NewWriter(i.sess)
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

func (i *InteractiveHandler) displayNodes(nodes []sdk.Node) {
	tree := ConstructAssetNodeTree(nodes)
	tipHeaderMsg := "\r\nNode: [ ID.Name(Asset amount) ]"
	tipEndMsg := "Tips: Enter g+NodeID to display the host under the node, such as g1\r\n\r"

	_, err := io.WriteString(i.sess, tipHeaderMsg)
	_, err = io.WriteString(i.sess, tree.String())
	_, err = io.WriteString(i.sess, tipEndMsg)
	if err != nil {
		logger.Info("displayAssetNodes err:", err)
	}

}

func (i *InteractiveHandler) refreshAssetsAndNodesData() {
	_, err := io.WriteString(i.sess, "Refresh done\r\n")
	if err != nil {
		logger.Error("refresh Assets  Nodes err:", err)
	}
}

func (i *InteractiveHandler) loadUserAssets() {
	i.assets = service.GetUserAssets(i.user.Id)
}

func (i *InteractiveHandler) loadUserAssetNodes() {
	i.nodes = service.GetUserNodes(i.user.Id)
}

func (i *InteractiveHandler) changeLanguage() {

}

func (i *InteractiveHandler) JoinShareRoom(roomID string) {
	sshConn := userhome.NewSSHConn(i.sess)
	ctx, cancelFuc := context.WithCancel(i.sess.Context())

	_, winCh, _ := i.sess.Pty()
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case win, ok := <-winCh:
				if !ok {
					return
				}
				fmt.Println("join term change:", win)
			}
		}
	}()
	proxy.Manager.JoinShareRoom(roomID, sshConn)
	logger.Info("exit room id:", roomID)
	cancelFuc()

}

func (i *InteractiveHandler) searchAsset(key string) (assets []sdk.Asset) {
	//if indexNum, err := strconv.Atoi(key); err == nil {
	//	if indexNum > 0 && indexNum <= len(i.searchResult) {
	//		return []sdk.Asset{i.searchResult[indexNum-1]}
	//	}
	//}
	//
	//if assetsData, ok := i.assetData.Load(AssetsMapKey); ok {
	//	for _, assetValue := range assetsData.([]sdk.Asset) {
	//		if isSubstring([]string{assetValue.Ip, assetValue.Hostname, assetValue.Comment}, key) {
	//			assets = append(assets, assetValue)
	//		}
	//	}
	//} else {
	//	assetsData, _ := Cached.Load(i.user.Id)
	//	for _, assetValue := range assetsData.([]sdk.Asset) {
	//		if isSubstring([]string{assetValue.Ip, assetValue.Hostname, assetValue.Comment}, key) {
	//			assets = append(assets, assetValue)
	//		}
	//	}
	//}

	return assets
}

func (i *InteractiveHandler) searchNodeAssets(num int) (assets []sdk.Asset) {
	//var assetNodesData []sdk.Node
	//if assetNodes, ok := i.assetData.Load(AssetNodesMapKey); ok {
	//	assetNodesData = assetNodes.([]sdk.Node)
	//	if num > len(assetNodesData) || num == 0 {
	//		return assets
	//	}
	//	return assetNodesData[num-1].AssetsGranted
	//}
	return assets

}

func (i *InteractiveHandler) Proxy(asset sdk.Asset, systemUser sdk.SystemUserAuthInfo) error {
	return nil
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
//		Port:      asset.Port,
//		UserName:  systemUser.UserName,
//		Password:  systemUser.Password,
//		PublicKey: parsePrivateKey(systemUser.PrivateKey)}
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
//			"user":        i.user.UserName,
//			"asset":       asset.Hostname,
//			"org_id":      asset.OrgID,
//			"system_user": systemUser.UserName,
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
//		"user":        i.user.UserName,
//		"asset":       asset.Hostname,
//		"org_id":      asset.OrgID,
//		"system_user": systemUser.UserName,
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
//	err = proxy.Manager.Switch(i.sess.Context(), Home, memChan)
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
func ConstructAssetNodeTree(assetNodes []sdk.Node) treeprint.Tree {
	sdk.SortAssetNodesByKey(assetNodes)
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

func SessionHandler(sess ssh.Session) {
	_, _, ptyOk := sess.Pty()
	if ptyOk {
		handler := &InteractiveHandler{
			sess: sess,
			term: terminal.NewTerminal(sess, "Opt> "),
		}

		logger.Info("Accept one session")
		handler.Dispatch()
	} else {
		_, err := io.WriteString(sess, "No PTY requested.\n")
		if err != nil {
			return
		}
	}
}
