package exchange

import (
	"io"
	"sync"

	"github.com/mediocregopher/radix/v3"

	"github.com/jumpserver/koko/pkg/logger"
	"github.com/jumpserver/koko/pkg/model"
)

var _ io.WriteCloser = (*redisChannel)(nil)

type redisChannel struct {
	roomId string

	writeChannel string

	readChannel string

	pubSub radix.PubSubConn

	manager *redisRoomManager

	subMsgCh chan radix.PubSubMessage

	once sync.Once

	errMsg error

	done chan struct{}

	count chan int
}

func (s *redisChannel) Write(p []byte) (int, error) {
	dataMsg := model.RoomMessage{
		Event: model.DataEvent,
		Body:  p,
	}
	err := s.sendMessage(&dataMsg)
	return len(p), err
}

func (s *redisChannel) sendMessage(msg *model.RoomMessage) error {
	err := s.manager.publishCommand(s.writeChannel, msg.Marshal())
	if err != nil {
		logger.Errorf("Redis send message to room %s err: %s", s.roomId, err)
	}
	return err
}

func (s *redisChannel) Close() error {
	s.once.Do(func() {
		if err := s.pubSub.Unsubscribe(s.subMsgCh, s.readChannel); err != nil {
			logger.Errorf("Redis unsubscribe channel %s err: %s", s.readChannel, err)
		}
		s.errMsg = s.pubSub.Close()
		close(s.subMsgCh)
		close(s.done)
		logger.Infof("Redis channel for room %s closed", s.roomId)
	})

	return s.errMsg
}

func (s *redisChannel) addSubscribeCount(i int) {
	select {
	case <-s.done:
	case s.count <- i:
	}
}
