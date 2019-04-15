package sshd

import (
	"cocogo/pkg/core"
	"cocogo/pkg/model"
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"

	"github.com/olekukonko/tablewriter"

	"github.com/xlab/treeprint"

	"github.com/gliderlabs/ssh"
	"golang.org/x/crypto/ssh/terminal"
)

type HelpInfo struct {
	UserName  string
	ColorCode string
	ColorEnd  string
	Tab       string
	EndLine   string
}

func (d HelpInfo) displayHelpInfo(sess ssh.Session) {
	e := displayTemplate.Execute(sess, d)
	if e != nil {
		log.Warn("display help info failed")
	}
}

type sshInteractive struct {
	sess                ssh.Session
	term                *terminal.Terminal
	assetData           sync.Map
	user                model.User
	helpInfo            HelpInfo
	currentSearchAssets []model.Asset
	onceLoad            sync.Once
	sync.RWMutex
}

func (s *sshInteractive) displayHelpInfo() {
	s.helpInfo.displayHelpInfo(s.sess)
}

func (s *sshInteractive) chooseSystemUser(systemUsers []model.SystemUser) model.SystemUser {
	table := tablewriter.NewWriter(s.sess)
	table.SetHeader([]string{"ID", "UserName"})
	for i := 0; i < len(systemUsers); i++ {
		table.Append([]string{strconv.Itoa(i + 1), systemUsers[i].UserName})
	}
	table.SetBorder(false)
	count := 0
	term := terminal.NewTerminal(s.sess, "num:")
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
func (s *sshInteractive) displayAssetsOrProxy(assets []model.Asset) {
	if len(assets) == 1 {
		var systemUser model.SystemUser
		switch len(assets[0].SystemUsers) {
		case 0:
			// 有授权的资产，但是资产用户信息，无法登陆
			s.displayAssets(assets)
			return
		case 1:
			systemUser = assets[0].SystemUsers[0]
		default:
			systemUser = s.chooseSystemUser(assets[0].SystemUsers)
		}

		authInfo, err := appService.GetSystemUserAssetAuthInfo(systemUser.Id, assets[0].Id)
		if err != nil {
			return
		}
		if ok := appService.ValidateUserAssetPermission(s.user.Id, systemUser.Id, assets[0].Id); !ok {
			// 检查user 是否对该资产有权限
			return
		}

		err = s.Proxy(assets[0], authInfo)
		if err != nil {
			log.Info(err)
		}
		return
	} else {
		s.displayAssets(assets)
	}
}

func (s *sshInteractive) displayAssets(assets []model.Asset) {
	if len(assets) == 0 {
		_, _ = io.WriteString(s.sess, "\r\n No Assets\r\n\r")
	} else {
		table := tablewriter.NewWriter(s.sess)
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

func (s *sshInteractive) displayAssetNodes(nodes []model.AssetNode) {
	tree := ConstructAssetNodeTree(nodes)
	tipHeaderMsg := "\r\nNode: [ ID.Name(Asset amount) ]"
	tipEndMsg := "Tips: Enter g+NodeID to display the host under the node, such as g1\r\n\r"

	_, err := io.WriteString(s.sess, tipHeaderMsg)
	_, err = io.WriteString(s.sess, tree.String())
	_, err = io.WriteString(s.sess, tipEndMsg)
	if err != nil {
		log.Info("displayAssetNodes err:", err)
	}

}

func (s *sshInteractive) refreshAssetsAndNodesData() {
	s.loadUserAssets()
	s.loadUserAssetNodes()
	_, err := io.WriteString(s.sess, "Refresh done\r\n")
	if err != nil {
		log.Error("refresh Assets  Nodes err:", err)
	}

}

func (s *sshInteractive) loadUserAssets() {
	assets, err := appService.GetUserAssets(s.user.Id)
	if err != nil {
		log.Error("load Assets failed")
		return
	}
	log.Info("load Assets success")
	Cached.Store(s.user.Id, assets)
	s.assetData.Store(AssetsMapKey, assets)
}

func (s *sshInteractive) loadUserAssetNodes() {
	assetNodes, err := appService.GetUserAssetNodes(s.user.Id)
	if err != nil {
		log.Error("load Asset Nodes failed")
		return
	}
	log.Info("load Asset Nodes success")
	s.assetData.Store(AssetNodesMapKey, assetNodes)
}

func (s *sshInteractive) changeLanguage() {

}

func (s *sshInteractive) JoinShareRoom(roomID string) {
	sshConn := &SSHConn{
		conn: s.sess,
		uuid: generateNewUUID(),
	}
	ctx, cancelFuc := context.WithCancel(s.sess.Context())

	_, winCh, _ := s.sess.Pty()
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
	core.Manager.JoinShareRoom(roomID, sshConn)
	log.Info("exit room id:", roomID)
	cancelFuc()

}

func (s *sshInteractive) StartDispatch() {
	_, winCh, _ := s.sess.Pty()
	for {
		ctx, cancelFuc := context.WithCancel(s.sess.Context())
		go func() {

			for {
				select {
				case <-ctx.Done():
					log.Info("ctx done")
					return
				case win, ok := <-winCh:
					if !ok {
						return
					}
					log.Info("InteractiveHandler term change:", win)
					_ = s.term.SetSize(win.Width, win.Height)
				}
			}
		}()
		line, err := s.term.ReadLine()
		cancelFuc()
		if err != nil {
			log.Error("ReadLine done", err)
			break
		}
		if line == "" {
			continue
		}

		s.onceLoad.Do(func() {
			if _, ok := Cached.Load(s.user.Id); !ok {
				s.loadUserAssets()
				s.loadUserAssetNodes()

			} else {
				log.Info("first load this user asset data ")
				go func() {
					s.loadUserAssets()
					s.loadUserAssetNodes()
				}()
			}
		})

		if len(line) == 1 {
			switch line {
			case "p", "P":
				if assets, ok := s.assetData.Load(AssetsMapKey); ok {
					s.displayAssets(assets.([]model.Asset))
					s.currentSearchAssets = assets.([]model.Asset)
				} else if assets, _ := Cached.Load(s.user.Id); ok {
					s.displayAssets(assets.([]model.Asset))
					s.currentSearchAssets = assets.([]model.Asset)
				}

			case "g", "G":
				if assetNodes, ok := s.assetData.Load(AssetNodesMapKey); ok {
					s.displayAssetNodes(assetNodes.([]model.AssetNode))
				} else {
					s.displayAssetNodes([]model.AssetNode{})
				}
			case "s", "S":
				s.changeLanguage()
			case "h", "H":
				s.displayHelpInfo()
			case "r", "R":
				s.refreshAssetsAndNodesData()
			case "q", "Q":
				log.Info("exit session")
				return
			default:
				assets := s.searchAsset(line)
				s.currentSearchAssets = assets
				s.displayAssetsOrProxy(assets)
			}
			continue
		}
		if strings.Index(line, "/") == 0 {
			searchWord := strings.TrimSpace(strings.TrimPrefix(line, "/"))
			assets := s.searchAsset(searchWord)
			s.currentSearchAssets = assets
			s.displayAssets(assets)
			continue
		}

		if strings.Index(line, "g") == 0 {
			searchWord := strings.TrimSpace(strings.TrimPrefix(line, "g"))
			if num, err := strconv.Atoi(searchWord); err == nil {
				if num >= 0 {
					assets := s.searchNodeAssets(num)
					s.displayAssets(assets)
					s.currentSearchAssets = assets
					continue
				}
			}
		}

		if strings.Index(line, "join") == 0 {
			roomID := strings.TrimSpace(strings.TrimPrefix(line, "join"))
			s.JoinShareRoom(roomID)
			continue
		}

		assets := s.searchAsset(line)
		s.currentSearchAssets = assets
		s.displayAssetsOrProxy(assets)

	}
}

func (s *sshInteractive) searchAsset(key string) (assets []model.Asset) {
	if indexNum, err := strconv.Atoi(key); err == nil {
		if indexNum > 0 && indexNum <= len(s.currentSearchAssets) {
			return []model.Asset{s.currentSearchAssets[indexNum-1]}
		}
	}

	if assetsData, ok := s.assetData.Load(AssetsMapKey); ok {
		for _, assetValue := range assetsData.([]model.Asset) {
			if isSubstring([]string{assetValue.Ip, assetValue.Hostname, assetValue.Comment}, key) {
				assets = append(assets, assetValue)
			}
		}
	} else {
		assetsData, _ := Cached.Load(s.user.Id)
		for _, assetValue := range assetsData.([]model.Asset) {
			if isSubstring([]string{assetValue.Ip, assetValue.Hostname, assetValue.Comment}, key) {
				assets = append(assets, assetValue)
			}
		}
	}

	return assets
}

func (s *sshInteractive) searchNodeAssets(num int) (assets []model.Asset) {
	var assetNodesData []model.AssetNode
	if assetNodes, ok := s.assetData.Load(AssetNodesMapKey); ok {
		assetNodesData = assetNodes.([]model.AssetNode)
		if num > len(assetNodesData) || num == 0 {
			return assets
		}
		return assetNodesData[num-1].AssetsGranted
	}
	return assets

}

func (s *sshInteractive) Proxy(asset model.Asset, systemUser model.SystemUserAuthInfo) error {
	/*
		1. 创建SSHConn，符合core.Conn接口
		2. 创建一个session Home
		3. 创建一个NodeConn，及相关的channel 可以是MemoryChannel 或者是redisChannel
		4. session Home 与 proxy channel 交换数据
	*/
	sshConn := &SSHConn{
		conn: s.sess,
		uuid: generateNewUUID(),
	}
	serverAuth := core.ServerAuth{
		IP:        asset.Ip,
		Port:      asset.Port,
		UserName:  systemUser.UserName,
		Password:  systemUser.Password,
		PublicKey: parsePrivateKey(systemUser.PrivateKey)}

	nodeConn, err := core.NewNodeConn(serverAuth, sshConn)
	if err != nil {
		log.Error(err)
		return err
	}
	defer nodeConn.Close()

	memChan := core.NewMemoryChannel(nodeConn, sshConn)

	userHome := core.NewUserSessionHome(sshConn)
	log.Info("session Home ID: ", userHome.SessionID())

	err = core.Manager.Switch(context.TODO(), userHome, memChan)
	if err != nil {
		log.Error(err)
	}
	return err

}

func isSubstring(sArray []string, substr string) bool {
	for _, s := range sArray {
		if strings.Contains(s, substr) {
			return true
		}
	}
	return false
}

func ConstructAssetNodeTree(assetNodes []model.AssetNode) treeprint.Tree {
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
