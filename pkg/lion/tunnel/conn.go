package tunnel

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"

	"github.com/jumpserver/koko/pkg/config"
	"github.com/jumpserver/koko/pkg/lion/guacd"
	"github.com/jumpserver/koko/pkg/lion/session"
	"github.com/jumpserver/koko/pkg/logger"

	"github.com/jumpserver-dev/sdk-go/common"
	"github.com/jumpserver-dev/sdk-go/model"
)

const (
	INTERNALDATAOPCODE = ""
	PINGOPCODE         = "ping"
)

var _ sort.Interface = Connections{}

type Connections []*Connection

func (c Connections) Len() int {
	return len(c)
}
func (c Connections) Less(i, j int) bool {
	iCreated := c[i].Sess.Created.UTC()
	jCreated := c[j].Sess.Created.UTC()
	return iCreated.Before(jCreated)
}

func (c Connections) Swap(i, j int) {
	c[j], c[i] = c[i], c[j]
}

type Connection struct {
	Sess        *session.TunnelSession
	guacdTunnel *guacd.Tunnel
	Service     *session.Server

	guacdAddr string

	ws *websocket.Conn

	wsLock    sync.Mutex
	guacdLock sync.Mutex

	outputFilter *OutputStreamInterceptingFilter

	inputFilter *InputStreamInterceptingFilter

	clipboardFilter *clipboardPolicyFilter

	done chan struct{}

	traceLock sync.Mutex
	traceMap  map[*guacd.Tunnel]struct{}

	lockedStatus atomic.Bool
	operatorUser atomic.Value

	recordStatus atomic.Bool

	Cache GuaTunnelCache
	meta  *MetaShareUserMessage

	currentOnlineUsers map[string]MetaShareUserMessage

	invalidPerm     atomic.Bool
	invalidPermData []byte
	invalidPermTime time.Time
}

func (t *Connection) SendWsMessage(msg guacd.Instruction) error {
	return t.writeWsMessage([]byte(msg.String()))
}

func (t *Connection) Close() {
	t.releaseMonitorTunnel()
	if t.ws != nil {
		_ = t.ws.Close()
	}
	if t.guacdTunnel != nil {
		_ = t.guacdTunnel.Close()
	}
}

func (t *Connection) writeWsMessage(p []byte) error {
	t.wsLock.Lock()
	defer t.wsLock.Unlock()
	return t.ws.WriteMessage(websocket.TextMessage, p)
}

func (t *Connection) WriteTunnelMessage(msg guacd.Instruction) (err error) {
	_, err = t.writeTunnelMessage([]byte(msg.String()))
	return err
}

func (t *Connection) writeTunnelMessage(p []byte) (int, error) {
	t.guacdLock.Lock()
	defer t.guacdLock.Unlock()
	return t.guacdTunnel.WriteAndFlush(p)
}

func (t *Connection) readTunnelInstruction() (*guacd.Instruction, error) {
	for {
		instruction, err := t.guacdTunnel.ReadInstruction()
		if err != nil {
			return nil, err
		}
		newInstruction := &instruction
		if t.inputFilter != nil {
			newInstruction = t.inputFilter.Filter(newInstruction)
			if newInstruction == nil {
				continue
			}
		}
		if t.outputFilter != nil {
			newInstruction = t.outputFilter.Filter(newInstruction)
			if newInstruction == nil {
				continue
			}
		}
		if t.clipboardFilter != nil {
			newInstruction = t.clipboardFilter.filterToClient(newInstruction)
			if newInstruction == nil {
				continue
			}
		}
		return newInstruction, nil
	}

}

func (t *Connection) Run(ctx *gin.Context) (err error) {
	defer t.releaseMonitorTunnel()
	// 需要发送 uuid 返回给 guacamole tunnel
	err = t.SendWsMessage(guacd.NewInstruction(
		INTERNALDATAOPCODE, t.guacdTunnel.UUID()))
	if err != nil {
		logger.Errorf("Run err: %s", err)
		return err
	}
	eventChan := t.Cache.GetSessionEventChan(t.Sess.ID)
	var jsonBuilder strings.Builder
	_ = json.NewEncoder(&jsonBuilder).Encode(t.meta)
	metaJsonStr := jsonBuilder.String()
	currentUserInst := NewJmsEventInstruction("current_user", metaJsonStr)
	_ = t.SendWsMessage(currentUserInst)
	eventData := []byte(metaJsonStr)
	t.Cache.BroadcastSessionEvent(t.Sess.ID, &Event{Type: ShareJoin, Data: eventData})
	defer func() {
		t.Cache.BroadcastSessionEvent(t.Sess.ID, &Event{Type: ShareExit, Data: eventData})
	}()

	parser := t.Service.GetFilterParser(t.Sess)
	userInputMessageChan := make(chan *session.Message, 1)
	defer func() {
		parser.Close()
	}()
	// 处理数据流
	parser.ParseStream(userInputMessageChan)
	// 记录命令
	cmdChan := parser.CommandRecordChan()
	go t.recordCommand(cmdChan)

	meta := session.MetaMessage{
		UserId:  t.Sess.User.ID,
		User:    t.Sess.User.String(),
		Created: common.NewNowUTCTime().String(),
	}
	exit := make(chan error, 2)
	activeChan := make(chan struct{})
	noNopTime := time.Now()
	maxNopTimeout := time.Minute * 5
	var requiredErr guacd.Instruction
	go func(t *Connection) {
		for {
			instruction, err := t.readTunnelInstruction()
			if err != nil {
				logger.Errorf("Session[%s] guacamole server read err: %+v", t, err)
				exit <- err
				break
			}

			switch instruction.Opcode {
			case guacd.InstructionServerDisconnect,
				guacd.InstructionServerError:
				logger.Infof("Session[%s] receive guacamole server disconnect: %s", t, instruction.String())
			case guacd.InstructionStreamingAck:
				select {
				case activeChan <- struct{}{}:
				default:
				}
			}

			switch instruction.Opcode {
			case guacd.InstructionClientNop:
				if time.Since(noNopTime) > maxNopTimeout {
					logger.Errorf("Session[%s] guacamole server nop timeout", t)
					if requiredErr.Opcode != "" {
						logger.Errorf("Session[%s] send guacamole server required err: %s", t,
							requiredErr.String())
						_ = t.writeWsMessage([]byte(requiredErr.String()))
						requiredErr = guacd.Instruction{}
						continue
					}

				}
			case guacd.InstructionRequired:
				msg := fmt.Sprintf("required: %s", strings.Join(instruction.Args, ","))
				logger.Infof("Session[%s] receive guacamole server required: %s", t, msg)
				requiredErr = guacd.NewInstruction(guacd.InstructionServerError, msg)
				logger.Errorf("Session[%s] send guacamole server required err: %s", t,
					requiredErr.String())
				_ = t.writeWsMessage([]byte(requiredErr.String()))
				continue
			default:
				noNopTime = time.Now()
			}

			if err = t.writeWsMessage([]byte(instruction.String())); err != nil {
				logger.Errorf("Session[%s] send web client err: %+v", t, err)
				exit <- err
				break
			}
		}
		_ = t.ws.Close()
	}(t)

	go func(t *Connection) {
		for {
			_, message, err1 := t.ws.ReadMessage()
			if err1 != nil {
				if websocket.IsCloseError(err1, websocket.CloseNoStatusReceived) {
					logger.Warnf("Session[%s] web client read err: %+v", t, err1)
				} else {
					logger.Errorf("Session[%s] web client read err: %+v", t, err1)
				}
				exit <- err1
				break
			}

			if ret, err2 := guacd.ParseInstructionString(string(message)); err2 == nil {
				if ret.Opcode == INTERNALDATAOPCODE && len(ret.Args) >= 2 && ret.Args[0] == PINGOPCODE {
					if err3 := t.SendWsMessage(guacd.NewInstruction(INTERNALDATAOPCODE, PINGOPCODE)); err3 != nil {
						logger.Errorf("Session[%s] unable to send 'ping' response for WebSocket tunnel: %+v",
							t, err3)
					}
					continue
				}

				if t.lockedStatus.Load() {
					switch ret.Opcode {
					case guacd.InstructionClientSync,
						guacd.InstructionClientNop,
						guacd.InstructionStreamingAck:
					default:
						select {
						case activeChan <- struct{}{}:
						default:
						}
						logger.Infof("Session[%s] in locked status drop receive web client message opcode[%s]",
							t, ret.Opcode)
						continue
					}
					_, err4 := t.writeTunnelMessage(message)
					if err4 != nil {
						logger.Errorf("Session[%s] guacamole server write err: %+v", t, err4)
						exit <- err4
						break
					}
					logger.Debugf("Session[%s] send guacamole server message when locked status", t)
					continue
				}

				switch ret.Opcode {
				case guacd.InstructionKey:
					userInputMessageChan <- &session.Message{
						Opcode: ret.Opcode, Body: ret.Args,
						Meta: meta}
				case "INPUT_ACTIVE":
					select {
					case activeChan <- struct{}{}:
					default:
					}
					continue
				default:

				}

				switch ret.Opcode {
				case guacd.InstructionClientSync,
					guacd.InstructionClientNop,
					guacd.InstructionStreamingAck:
				case guacd.InstructionClientDisconnect:
					logger.Infof("Session[%s] receive web client disconnect opcode", t)
				default:
					select {
					case activeChan <- struct{}{}:
					default:
					}
				}
				if t.clipboardFilter != nil {
					filtered := t.clipboardFilter.filterToServer(&ret)
					if filtered == nil {
						continue
					}
					message = []byte(filtered.String())
				}
			} else {
				logger.Errorf("Session[%s] parse instruction err %s", t, err2)
			}
			_, err = t.writeTunnelMessage(message)
			if err != nil {
				logger.Errorf("Session[%s] guacamole server write err: %+v", t, err)
				exit <- err
				break
			}
		}
	}(t)
	maxIndexTime := t.Sess.TerminalConfig.MaxIdleTime
	maxSessionTimeInt := t.Sess.TerminalConfig.MaxSessionTime
	maxSessionDuration := time.Duration(maxSessionTimeInt) * time.Hour
	maxSessionTime := time.Now().Add(maxSessionDuration)
	maxIdleMinutes := time.Duration(maxIndexTime) * time.Minute
	activeDetectTicker := time.NewTicker(time.Minute)
	defer activeDetectTicker.Stop()
	latestActive := time.Now()

	for {
		select {
		case event := <-eventChan.eventCh:
			go t.handleEvent(event)
			continue
		case err = <-exit:
			logger.Infof("Session[%s] Connection exit %+v", t, err)
			if !t.recordStatus.Load() {
				reason := model.SessionLifecycleLog{Reason: string(model.ReasonErrConnectDisconnect)}
				t.Service.RecordLifecycleLog(t.Sess.ID, model.AssetConnectFinished, reason)
			}
			return err
		case <-ctx.Request.Context().Done():
			_ = t.ws.Close()
			_ = t.guacdTunnel.Close()
			reason := model.SessionLifecycleLog{Reason: string(model.ReasonErrConnectDisconnect)}
			t.Service.RecordLifecycleLog(t.Sess.ID, model.AssetConnectFinished, reason)
			logger.Errorf("Session[%s] request ctx done", t)
			return nil
		case <-activeChan:
			latestActive = time.Now()
		case detectTime := <-activeDetectTicker.C:
			if detectTime.After(maxSessionTime) {
				errSession := NewJMSMaxSessionTimeError(t.Sess.TerminalConfig.MaxSessionTime)
				_ = t.SendWsMessage(errSession.Instruction())
				logger.Errorf("Session[%s] terminated by max session time %d hour",
					t, maxSessionTimeInt)
				reason := model.SessionLifecycleLog{Reason: string(model.ReasonErrMaxSessionTimeout)}
				t.Service.RecordLifecycleLog(t.Sess.ID, model.AssetConnectFinished, reason)
				return nil
			}
			if detectTime.After(latestActive.Add(maxIdleMinutes)) {
				errIdle := NewJMSIdleTimeOutError(maxIndexTime)
				_ = t.SendWsMessage(errIdle.Instruction())
				logger.Errorf("Session[%s] terminated by %d min timeout",
					t, maxIndexTime)
				reason := model.SessionLifecycleLog{Reason: string(model.ReasonErrIdleDisconnect)}
				t.Service.RecordLifecycleLog(t.Sess.ID, model.AssetConnectFinished, reason)
				return nil
			}
			if t.IsPermissionExpired(detectTime) {
				_ = t.SendWsMessage(ErrPermissionExpired.Instruction())
				logger.Errorf("Session[%s] permission has expired", t)
				reason := model.SessionLifecycleLog{Reason: string(model.ReasonErrPermissionExpired)}
				t.Service.RecordLifecycleLog(t.Sess.ID, model.AssetConnectFinished, reason)
				return nil
			}
		}
	}

}

func (t *Connection) HandleTask(task *model.TerminalTask) error {
	switch task.Name {
	case model.TaskUnlockSession:
		t.lockedStatus.Store(false)
		t.operatorUser.Store(task.Kwargs.CreatedByUser)
		data := map[string]interface{}{
			"user": task.Kwargs.CreatedByUser,
		}
		p, _ := json.Marshal(data)
		ins := NewJmsEventInstruction("session_resume", string(p))
		_ = t.SendWsMessage(ins)
		t.notifySessionAction(ShareSessionResume, task.Kwargs.CreatedByUser)
	case model.TaskLockSession:
		t.lockedStatus.Store(true)
		t.operatorUser.Store(task.Kwargs.CreatedByUser)
		data := map[string]interface{}{
			"user": task.Kwargs.CreatedByUser,
		}
		p, _ := json.Marshal(data)
		ins := NewJmsEventInstruction("session_pause", string(p))
		_ = t.SendWsMessage(ins)
		t.notifySessionAction(ShareSessionPause, task.Kwargs.CreatedByUser)
	case model.TaskKillSession:
		t.recordStatus.Store(true)
		username := task.Kwargs.TerminatedBy
		ins := NewJMSGuacamoleError(1005, username)
		_ = t.SendWsMessage(ins.Instruction())
		reason := model.SessionLifecycleLog{Reason: string(model.ReasonErrAdminTerminate)}
		t.Service.RecordLifecycleLog(t.Sess.ID, model.AssetConnectFinished, reason)
	case model.TaskPermExpired:
		t.PermBecomeExpired(task.Name, task.Args)
	case model.TaskPermValid:
		t.PermBecomeValid(task.Name, task.Args)
	default:
		return fmt.Errorf("unknown task %s", task.Name)
	}
	logger.Infof("Session[%s] handle task %s", t, task.Name)
	return nil
}

func (t *Connection) String() string {
	return t.Sess.ID
}

func (t *Connection) IsPermissionExpired(now time.Time) bool {
	if t.Sess.ExpireInfo.IsExpired(now) {
		return true
	}
	if t.invalidPerm.Load() {
		maxInvalidTime := t.invalidPermTime.Add(10 * time.Minute)
		return now.After(maxInvalidTime)
	}
	return false
}

func (t *Connection) CloneMonitorTunnel() (*guacd.Tunnel, error) {
	info := guacd.NewClientInformation()
	conf := guacd.NewConfiguration()
	conf.ConnectionID = t.guacdTunnel.UUID()
	guacdAddr := t.guacdAddr
	monitorTunnel, err := guacd.NewTunnel(guacdAddr, conf, info)
	if err != nil {
		return nil, err
	}
	t.traceMonitorTunnel(monitorTunnel)
	return monitorTunnel, err
}

func (t *Connection) traceMonitorTunnel(monitorTunnel *guacd.Tunnel) {
	t.traceLock.Lock()
	defer t.traceLock.Unlock()
	if t.traceMap == nil {
		t.traceMap = make(map[*guacd.Tunnel]struct{})
	}
	t.traceMap[monitorTunnel] = struct{}{}
}

func (t *Connection) releaseMonitorTunnel() {
	t.traceLock.Lock()
	defer t.traceLock.Unlock()
	if t.traceMap == nil {
		return
	}
	for tunneler := range t.traceMap {
		_ = tunneler.Close()
	}
}

func (t *Connection) unTraceMonitorTunnel(monitorTunnel *guacd.Tunnel) {
	t.traceLock.Lock()
	defer t.traceLock.Unlock()
	if t.traceMap == nil {
		return
	}
	delete(t.traceMap, monitorTunnel)
}

func (t *Connection) recordCommand(cmdRecordChan chan *session.ExecutedCommand) {
	// 命令记录
	cmdRecorder := t.Service.GetCommandRecorder(t.Sess)
	for item := range cmdRecordChan {
		if item.Command == "" {
			continue
		}
		if config.GetConf().DisableKeyboardRecord {
			logger.Info("[tunnel] disable keyboard record")
			continue
		}
		cmd := t.generateCommandResult(item)
		cmdRecorder.Record(cmd)
	}
	// 关闭命令记录
	cmdRecorder.End()
}

// generateCommandResult 生成命令结果
func (t *Connection) generateCommandResult(item *session.ExecutedCommand) *model.Command {
	var (
		input  string
		output string
		user   string
	)
	user = item.User.User
	if len(item.Command) > 128 {
		input = item.Command[:128]
	} else {
		input = item.Command
	}
	return t.Service.GenerateCommandItem(t.Sess, user, input, output, item)
}

func (t *Connection) handleEvent(eventMsg *Event) {
	logger.Debugf("Session[%s] handle event: %s", t, eventMsg.Type)
	switch eventMsg.Type {
	case ShareJoin:
		var meta MetaShareUserMessage
		if err := json.Unmarshal(eventMsg.Data, &meta); err != nil {
			logger.Errorf("Session[%s] unmarshal meta message err: %s", t, err)
			return
		}
		key := meta.User + meta.Created
		t.traceLock.Lock()
		t.currentOnlineUsers[key] = meta
		t.traceLock.Unlock()
		defer t.notifyShareUsers()
		if t.meta.ShareId == meta.ShareId {
			logger.Info("Ignore self join event")
			return
		}
		if t.lockedStatus.Load() {
			user := t.operatorUser.Load().(string)
			defer t.notifySessionAction(ShareSessionPause, user)
		}
	case ShareExit:
		var meta MetaShareUserMessage
		if err := json.Unmarshal(eventMsg.Data, &meta); err != nil {
			logger.Errorf("Session[%s] unmarshal meta message err: %s", t, err)
			return
		}
		key := meta.User + meta.Created
		t.traceLock.Lock()
		delete(t.currentOnlineUsers, key)
		t.traceLock.Unlock()
		defer t.notifyShareUsers()
	case ShareUsers:
	case ShareRemoveUser,
		ShareSessionPause,
		ShareSessionResume:
		return
	}
	inst := NewJmsEventInstruction(eventMsg.Type, string(eventMsg.Data))
	_ = t.SendWsMessage(inst)
}

func (t *Connection) notifyShareUsers() {
	t.traceLock.Lock()
	body, _ := json.Marshal(t.currentOnlineUsers)
	t.traceLock.Unlock()
	t.Cache.BroadcastSessionEvent(t.Sess.ID, &Event{
		Type: ShareUsers,
		Data: body,
	})
}

func (t *Connection) notifySessionAction(action string, user string) {
	data := map[string]interface{}{
		"user": user,
	}
	p, _ := json.Marshal(data)
	t.Cache.BroadcastSessionEvent(t.Sess.ID,
		&Event{Type: action, Data: p})
}

func (t *Connection) PermBecomeExpired(code, detail string) {
	if t.invalidPerm.Load() {
		return
	}
	t.invalidPermTime = time.Now()
	t.invalidPerm.Store(true)
	p, _ := json.Marshal(map[string]string{"code": code, "detail": detail})
	t.invalidPermData = p
	t.Cache.BroadcastSessionEvent(t.Sess.ID,
		&Event{Type: PermExpiredEvent, Data: p})
}

func (t *Connection) PermBecomeValid(code, detail string) {
	if !t.invalidPerm.Load() {
		return
	}
	t.invalidPermTime = time.Now()
	t.invalidPerm.Store(false)
	p, _ := json.Marshal(map[string]string{"code": code, "detail": detail})
	t.invalidPermData = p
	t.Cache.BroadcastSessionEvent(t.Sess.ID,
		&Event{Type: PermValidEvent, Data: p})
}
