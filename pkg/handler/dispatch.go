package handler

import (
	"io"
	"strconv"
	"strings"

	"github.com/jumpserver/koko/pkg/exchange"
	"github.com/jumpserver/koko/pkg/i18n"
	"github.com/jumpserver/koko/pkg/jms-sdk-go/model"
	"github.com/jumpserver/koko/pkg/logger"
)

func (h *InteractiveHandler) Dispatch() {
	defer logger.Infof("Request %s: User %s stop interactive", h.sess.ID(), h.user.Name)
	var initialed bool
	for {
		line, err := h.term.ReadLine()
		if err != nil {
			logger.Debugf("User %s close connect", h.user.Name)
			break
		}
		line = strings.TrimSpace(line)
		if len(line) == 0 {
			// 当 只是回车 空字符单独处理
			if initialed {
				h.selectHandler.MoveNextPage()
			} else {
				h.selectHandler.SetSelectType(TypeAsset)
				h.selectHandler.Search("")
			}
			initialed = true
			continue
		}
		initialed = true
		switch len(line) {
		case 1:
			switch strings.ToLower(line) {
			case "p":
				h.selectHandler.SetSelectType(TypeAsset)
				h.selectHandler.Search("")
				continue
			case "b":
				h.selectHandler.MovePrePage()
				continue
			case "d":
				h.selectHandler.SetSelectType(TypeDatabase)
				h.selectHandler.Search("")
				continue
			case "n":
				h.selectHandler.MoveNextPage()
				continue
			case "g":
				h.wg.Wait() // 等待node加载完成
				h.displayNodeTree(h.nodes)
				continue
			case "h":
				h.displayBanner()
				initialed = false
				continue
			case "r":
				h.refreshAssetsAndNodesData()
				continue
			case "q":
				logger.Infof("user %s enter %s to exit", h.user.Name, line)
				return
			case "k":
				h.selectHandler.SetSelectType(TypeK8s)
				h.selectHandler.Search("")
				continue
			}
		default:
			switch {
			case line == "exit", line == "quit":
				logger.Infof("user %s enter %s to exit", h.user.Name, line)
				return
			case strings.Index(line, "/") == 0:
				if strings.Index(line[1:], "/") == 0 {
					line = strings.TrimSpace(line[2:])
					h.selectHandler.SearchAgain(line)
					continue
				}
				line = strings.TrimSpace(line[1:])
				h.selectHandler.Search(line)
				continue
			case strings.Index(line, "g") == 0:
				searchWord := strings.TrimSpace(strings.TrimPrefix(line, "g"))
				if num, err := strconv.Atoi(searchWord); err == nil {
					h.wg.Wait() // 等待node加载完成
					if num > 0 && num <= len(h.nodes) {
						selectedNode := h.nodes[num-1]
						h.selectHandler.SetNode(selectedNode)
						h.selectHandler.Search("")
						continue
					}
				}
			case strings.Index(line, "join") == 0:
				roomID := strings.TrimSpace(strings.TrimPrefix(line, "join"))
				JoinRoom(h, roomID)
				continue
			}
		}
		h.selectHandler.SearchOrProxy(line)
	}
}

func (h *InteractiveHandler) displayNodeTree(nodes model.NodeList) {
	tree := ConstructNodeTree(nodes)
	_, _ = io.WriteString(h.term, "\n\r"+i18n.T("Node: [ ID.Name(Asset amount) ]"))
	_, _ = io.WriteString(h.term, tree.String())
	_, err := io.WriteString(h.term, i18n.T("Tips: Enter g+NodeID to display the host under the node, such as g1")+"\n\r")
	if err != nil {
		logger.Info("displayAssetNodes err:", err)
	}
}

func (h *InteractiveHandler) CheckShareRoomWritePerm(shareRoomID string) bool {
	// todo: check current user has pem to write
	return false
}

func (h *InteractiveHandler) CheckShareRoomReadPerm(shareRoomID string) bool {
	ret, err := h.jmsService.ValidateJoinSessionPermission(h.user.ID, shareRoomID)
	if err != nil {
		logger.Error(err)
		return false
	}
	return ret.Ok

}

func JoinRoom(h *InteractiveHandler, roomId string) {
	if room := exchange.GetRoom(roomId); room != nil {
		conn := exchange.WrapperUserCon(h.sess)
		room.Subscribe(conn)
		defer room.UnSubscribe(conn)
		for {
			buf := make([]byte, 1024)
			nr, err := h.sess.Read(buf)
			if nr > 0 && h.CheckShareRoomWritePerm(roomId) {
				room.Receive(&exchange.RoomMessage{
					Event: exchange.DataEvent, Body: buf[:nr]})
			}
			if err != nil {
				break
			}
		}
		logger.Infof("Conn[%s] user read end", h.sess.Uuid)
	}
}
