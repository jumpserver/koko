package tunnel

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/gorilla/websocket"

	"github.com/jumpserver/koko/pkg/lion/guacd"
	"github.com/jumpserver/koko/pkg/logger"

	"github.com/jumpserver-dev/sdk-go/model"
)

type MonitorCon struct {
	Id          string
	guacdTunnel Tunneler

	ws *websocket.Conn

	wsLock    sync.Mutex
	guacdLock sync.Mutex

	Service *GuacamoleTunnelServer
	User    *model.User
	Meta    *MetaShareUserMessage

	lockedStatus atomic.Bool
}

func (m *MonitorCon) SendWsMessage(msg guacd.Instruction) error {
	return m.writeWsMessage([]byte(msg.String()))
}

func (m *MonitorCon) writeWsMessage(p []byte) error {
	m.wsLock.Lock()
	defer m.wsLock.Unlock()
	return m.ws.WriteMessage(websocket.TextMessage, p)
}

func (m *MonitorCon) WriteTunnelMessage(msg guacd.Instruction) (err error) {
	_, err = m.writeTunnelMessage([]byte(msg.String()))
	return err
}

func (m *MonitorCon) writeTunnelMessage(p []byte) (int, error) {
	m.guacdLock.Lock()
	defer m.guacdLock.Unlock()
	return m.guacdTunnel.WriteAndFlush(p)
}
func (m *MonitorCon) readTunnelInstruction() (*guacd.Instruction, error) {
	instruction, err := m.guacdTunnel.ReadInstruction()
	if err != nil {
		return nil, err
	}
	return &instruction, nil
}

func (m *MonitorCon) Run(ctx context.Context) (err error) {
	retChan := m.Service.Cache.GetSessionEventChan(m.Id)
	if m.Meta != nil {
		var jsonBuilder strings.Builder
		_ = json.NewEncoder(&jsonBuilder).Encode(m.Meta)
		metaJsonStr := jsonBuilder.String()
		currentUserInst := NewJmsEventInstruction("current_user", metaJsonStr)
		_ = m.SendWsMessage(currentUserInst)
		eventData := []byte(metaJsonStr)
		m.Service.Cache.BroadcastSessionEvent(m.Id, &Event{Type: ShareJoin, Data: eventData})
		defer func() {
			m.Service.Cache.BroadcastSessionEvent(m.Id, &Event{Type: ShareExit, Data: eventData})
		}()
	}

	exit := make(chan error, 2)
	go func(t *MonitorCon) {
		for {
			instruction, err1 := t.readTunnelInstruction()
			if err1 != nil {
				_ = t.writeWsMessage([]byte(ErrDisconnect.String()))
				logger.Infof("Monitor[%s] guacd tunnel read err: %+v", t.Id, err1)
				exit <- err1
				break
			}
			if err2 := t.writeWsMessage([]byte(instruction.String())); err2 != nil {
				logger.Error(err2)
				exit <- err2
				break
			}
		}
		_ = t.ws.Close()
	}(m)

	go func(t *MonitorCon) {
		for {
			_, message, err1 := t.ws.ReadMessage()
			if err1 != nil {
				logger.Infof("Monitor[%s] ws read err: %+v", t.Id, err1)

				exit <- err1
				break
			}
			if ret, err2 := guacd.ParseInstructionString(string(message)); err2 == nil {
				if ret.Opcode == INTERNALDATAOPCODE && len(ret.Args) >= 2 && ret.Args[0] == PINGOPCODE {
					if err3 := t.SendWsMessage(guacd.NewInstruction(INTERNALDATAOPCODE, PINGOPCODE)); err3 != nil {
						logger.Error(err3)
					}
					continue
				}
				if t.lockedStatus.Load() {
					switch ret.Opcode {
					case guacd.InstructionClientSync,
						guacd.InstructionClientNop,
						guacd.InstructionStreamingAck:
					default:
						logger.Infof("Session[%s] in locked status drop receive web client message opcode[%s]",
							t.Id, ret.Opcode)
						continue
					}
					_, err4 := t.writeTunnelMessage(message)
					if err4 != nil {
						logger.Errorf("Session[%s] guacamole server write err: %+v", t.Id, err2)
						exit <- err4
						break
					}
					logger.Debugf("Session[%s] send guacamole server message when locked status", t.Id)
					continue
				}
			} else {
				logger.Errorf("Monitor[%s] parse instruction err %s", t.Id, err2)
			}
			_, err3 := t.writeTunnelMessage(message)
			if err3 != nil {
				logger.Errorf("Monitor[%s] guacamole tunnel write err: %+v", t.Id, err3)
				exit <- err3
				break
			}
		}
		_ = t.guacdTunnel.Close()
	}(m)

	for {
		select {
		case err = <-exit:
			logger.Infof("Monitor[%s] exit: %+v", m.Id, err)
			return err
		case <-ctx.Done():
			logger.Infof("Monitor[%s] done", m.Id)
			return nil
		case event := <-retChan.eventCh:
			if m.Meta == nil {
				logger.Debugf("Monitor[%s] do not need to handle event", m.Id)
				continue
			}
			go m.handleEvent(event)
			logger.Debugf("Monitor[%s] handle event: %s", m.Id, event.Type)
		}
	}
}

func (m *MonitorCon) handleEvent(eventMsg *Event) {
	logger.Debugf("Monitor[%s] handle event: %s", m.Id, eventMsg.Type)
	var inst guacd.Instruction
	switch eventMsg.Type {
	case ShareJoin:
		var meta MetaShareUserMessage
		_ = json.Unmarshal(eventMsg.Data, &meta)
		if m.Meta.ShareId == meta.ShareId {
			logger.Info("Ignore self join event")
			return
		}
		inst = NewJmsEventInstruction(ShareJoin, string(eventMsg.Data))
	case ShareExit:
		inst = NewJmsEventInstruction(ShareExit, string(eventMsg.Data))
	case ShareUsers:
		inst = NewJmsEventInstruction(ShareUsers, string(eventMsg.Data))
	case ShareRemoveUser:
		var removeData struct {
			User string               `json:"user"`
			Meta MetaShareUserMessage `json:"meta"`
		}
		_ = json.Unmarshal(eventMsg.Data, &removeData)
		if m.Meta.ShareId != removeData.Meta.ShareId {
			logger.Info("Ignore not self remove user event")
			return
		}
		errInst := NewJMSGuacamoleError(1011, removeData.User)
		inst = errInst.Instruction()
	case ShareSessionPause, ShareSessionResume:
		inst = NewJmsEventInstruction(eventMsg.Type, string(eventMsg.Data))
		locked := eventMsg.Type == ShareSessionPause
		m.lockedStatus.Store(locked)
	default:
		return
	}
	_ = m.SendWsMessage(inst)
}
