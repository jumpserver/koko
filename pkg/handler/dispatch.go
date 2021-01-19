package handler

import (
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/jumpserver/koko/pkg/exchange"
	"github.com/jumpserver/koko/pkg/i18n"
	"github.com/jumpserver/koko/pkg/logger"
	"github.com/jumpserver/koko/pkg/model"
	"github.com/jumpserver/koko/pkg/service"
	"github.com/jumpserver/koko/pkg/utils"
)

func (h *interactiveHandler) Dispatch() {
	defer logger.Infof("Request %s: User %s stop interactive", h.sess.ID(), h.user.Name)
	var currentApp Application
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
				currentApp = h.getAssetApp()
				currentApp.Search("")
				continue
			case "b":
				if currentApp != nil {
					currentApp.MovePrePage()
					continue
				}
			case "d":
				currentApp = h.getDatabaseApp()
				currentApp.Search("")
				continue
			case "n":
				if currentApp != nil {
					currentApp.MoveNextPage()
					continue
				}
			case "":
				if currentApp != nil {
					currentApp.MoveNextPage()
				} else {
					currentApp = h.getAssetApp()
					currentApp.Search("")
				}
				continue
			case "g":
				h.displayNodeTree(h.nodes)
				continue
			case "h":
				h.displayBanner()
				currentApp = nil
				continue
			case "r":
				h.refreshAssetsAndNodesData()
				continue
			case "q":
				logger.Infof("user %s enter %s to exit", h.user.Name, line)
				return
			case "k":
				currentApp = h.getK8sApp()
				currentApp.Search("")
				continue
			}
		default:
			switch {
			case line == "exit", line == "quit":
				logger.Infof("user %s enter %s to exit", h.user.Name, line)
				return
			case strings.Index(line, "/") == 0:
				if currentApp == nil {
					currentApp = h.getAssetApp()
				}
				if strings.Index(line[1:], "/") == 0 {
					line = strings.TrimSpace(line[2:])
					currentApp.SearchAgain(line)
					continue
				}
				line = strings.TrimSpace(line[1:])
				currentApp.Search(line)
				continue
			case strings.Index(line, "g") == 0:
				searchWord := strings.TrimSpace(strings.TrimPrefix(line, "g"))
				if num, err := strconv.Atoi(searchWord); err == nil {
					<-h.firstLoadDone
					if num > 0 && num <= len(h.nodes) {
						currentApp = h.getNodeAssetApp(h.nodes[num-1])
						currentApp.Search("")
						continue
					}
				}
			case strings.Index(line, "join") == 0:
				roomID := strings.TrimSpace(strings.TrimPrefix(line, "join"))
				JoinRoom(h, roomID)
				continue
			}
		}

		if currentApp == nil {
			currentApp = h.getAssetApp()
		}
		currentApp.SearchOrProxy(line)
	}
}

func (h *interactiveHandler) displayNodeTree(nodes model.NodeList) {
	tree := ConstructAssetNodeTree(nodes)
	_, _ = io.WriteString(h.term, "\n\r"+i18n.T("Node: [ ID.Name(Asset amount) ]"))
	_, _ = io.WriteString(h.term, tree.String())
	_, err := io.WriteString(h.term, i18n.T("Tips: Enter g+NodeID to display the host under the node, such as g1")+"\n\r")
	if err != nil {
		logger.Info("displayAssetNodes err:", err)
	}
}

func (h *interactiveHandler) getAssetApp() Application {
	var eng AssetEngine
	switch h.assetLoadPolicy {
	case "all":
		<-h.firstLoadDone
		eng = &localAssetEngine{
			data:     h.allAssets,
			pageInfo: &pageInfo{},
		}
	default:
		eng = &remoteAssetEngine{
			user:     h.user,
			pageInfo: &pageInfo{},
		}
	}
	app := AssetApplication{
		h:          h,
		engine:     eng,
		searchKeys: make([]string, 0),
	}
	h.term.SetPrompt("[Host]> ")
	return &app
}

func (h *interactiveHandler) getNodeAssetApp(node model.Node) Application {
	eng := &remoteNodeAssetEngine{
		user:     h.user,
		node:     node,
		pageInfo: &pageInfo{},
	}
	app := AssetApplication{
		h:          h,
		engine:     eng,
		searchKeys: make([]string, 0),
	}
	h.term.SetPrompt("[Host]> ")
	return &app
}

func (h *interactiveHandler) getDatabaseApp() Application {
	allDBs := service.GetUserDatabases(h.user.ID)
	eng := &localDatabaseEngine{
		data:     allDBs,
		pageInfo: &pageInfo{},
	}
	app := DatabaseApplication{
		h:          h,
		engine:     eng,
		searchKeys: make([]string, 0),
	}
	h.term.SetPrompt("[DB]> ")
	return &app
}

func (h *interactiveHandler) getK8sApp() Application {
	eng := &remoteK8sEngine{
		user:     h.user,
		pageInfo: &pageInfo{},
	}
	app := K8sApplication{
		h:          h,
		engine:     eng,
		searchKeys: make([]string, 0),
	}
	h.term.SetPrompt("[K8S]> ")
	return &app
}

func (h *interactiveHandler) CheckShareRoomWritePerm(shareRoomID string) bool {
	// todo: check current user has pem to write
	return false
}

func (h *interactiveHandler) CheckShareRoomReadPerm(shareRoomID string) bool {
	return service.JoinRoomValidate(h.user.ID, shareRoomID)
}

func JoinRoom(h *interactiveHandler, roomId string) {
	ex := exchange.GetExchange()
	roomChan := make(chan model.RoomMessage)
	room, err := ex.JoinRoom(roomChan, roomId)
	if err != nil {
		msg := fmt.Sprintf("Join room %s err: %s", roomId, err)
		utils.IgnoreErrWriteString(h.sess, msg)
		logger.Error(msg)
		return
	}
	defer ex.LeaveRoom(room, roomId)
	if !h.CheckShareRoomReadPerm(roomId) {
		utils.IgnoreErrWriteString(h.sess, fmt.Sprintf("Has no permission to join room %s\n", roomId))
		return
	}

	go func() {
		var exitMsg string
		for {
			msg, ok := <-roomChan
			if !ok {
				logger.Infof("User %s exit room %s by roomChan closed", h.user.Name, roomId)
				exitMsg = fmt.Sprintf("Room %s closed", roomId)
				break
			}
			switch msg.Event {
			case model.DataEvent, model.MaxIdleEvent, model.AdminTerminateEvent:
				_, _ = h.sess.Write(msg.Body)
				continue
			case model.LogoutEvent, model.ExitEvent:
				exitMsg = fmt.Sprintf("Session %s exit", roomId)
			case model.WindowsEvent, model.PingEvent:
				continue
			default:
				logger.Errorf("User %s in room %s receive unknown event %s", h.user.Name, roomId, msg.Event)
			}
			logger.Infof("User %s exit room  %s and stop to receive msg by %s", h.user.Name, roomId, msg.Event)
			break
		}
		_, _ = io.WriteString(h.sess, exitMsg)
		_ = h.sess.Close()
	}()
	buf := make([]byte, 1024)
	for {
		nr, err := h.sess.Read(buf)
		if err != nil {
			logger.Errorf("User %s exit room %s by %s", h.user.Name, roomId, err)
			break
		}
		if !h.CheckShareRoomWritePerm(roomId) {
			logger.Debugf("User %s has no perm to write and ignore data", h.user.Name)
			continue
		}

		msg := model.RoomMessage{
			Event: model.DataEvent,
			Body:  buf[:nr],
		}
		room.Publish(msg)
	}
}
