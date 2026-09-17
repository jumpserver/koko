package exchange

import (
	"encoding/json"

	"github.com/jumpserver/koko/pkg/logger"
)

func proxyRoom(room *Room, ch *redisChannel, userInputCh chan *RoomMessage) {
	defer func() {
		err := ch.Close() // 关闭连接
		if err != nil {
			logger.Errorf("Redis channel close err: %s", err)
		}
		select {
		case ch.manager.removeProxyRoomChan <- room:
		case <-ch.manager.done:
		}
		logger.Infof("Proxy redis room %s done", room.Id)
	}()
	for {
		select {
		case <-room.Done():
			logger.Infof("Redis room %s done", ch.roomId)
			return
		case <-ch.done:
			return
		case msg, ok := <-userInputCh:
			if !ok {
				return
			}
			if err := ch.sendMessage(msg); err != nil {
				logger.Errorf("Redis room %s send message err: %s", ch.roomId, err)
			}

		case redisMsg, ok := <-ch.subMsgCh:
			if !ok {
				logger.Infof("Redis room %s stop receive message", ch.roomId)
				return
			}
			var msg RoomMessage
			if err := json.Unmarshal([]byte(redisMsg.Payload), &msg); err != nil {
				logger.Errorf("Redis proxy room %s message unmarshal err: %s", ch.roomId, err)
				continue
			}
			room.broadcast(&msg)
		}
	}
}

// 接受其他 koko 的数据 给 Room
func proxyUserCon(room *Room, ch *redisChannel) {
	con := WrapperUserCon(ch)
	room.Subscribe(con)
	defer func() {
		room.UnSubscribe(con)
		err := ch.Close()
		if err != nil {
			logger.Errorf("Redis channel close err: %s", err)
		}
		select {
		case ch.manager.removeRedisUserConChan <- ch:
		case <-ch.manager.done:
		}
		logger.Infof("Proxy redis userCon for room %s done", room.Id)
	}()
	for {
		select {
		case <-room.Done():
			return
		case <-ch.done:
			return
		case redisMsg, ok := <-ch.subMsgCh:
			if !ok {
				logger.Infof("Redis proxy userCon for room %s stop receive message", ch.roomId)
				return
			}
			var msg RoomMessage
			if err := json.Unmarshal([]byte(redisMsg.Payload), &msg); err != nil {
				logger.Errorf("Redis proxy userCon %s message unmarshal err: %s", ch.roomId, err)
				continue
			}
			switch msg.Event {
			case ShareJoin, ShareLeave:
				room.Broadcast(&msg)
			case DataEvent:
				room.Receive(&msg)
			default:
				logger.Errorf("Redis proxy userCon %s ignore input event %s", ch.roomId, msg.Event)
			}
		}
	}
}
