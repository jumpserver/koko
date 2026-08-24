package exchange

import (
	"context"
	"io"
	"sync"

	"github.com/go-redis/redis/v8"

	"github.com/jumpserver/koko/pkg/logger"
)

var _ io.WriteCloser = (*redisChannel)(nil)

type redisChannel struct {
	roomId string

	writeChannel string

	readChannel string

	pubSub *redis.PubSub

	manager *redisRoomManager

	subMsgCh <-chan *redis.Message

	once sync.Once

	errMsg error

	done chan struct{}

	count chan int
}

func (s *redisChannel) Write(p []byte) (int, error) {
	dataMsg := RoomMessage{
		Event: DataEvent,
		Body:  p,
	}
	err := s.sendMessage(&dataMsg)
	return len(p), err
}

func (s *redisChannel) sendMessage(msg *RoomMessage) error {
	err := s.manager.publishCommand(s.writeChannel, msg.Marshal())
	if err != nil {
		logger.Errorf("Redis send message to room %s err: %s", s.roomId, err)
	}
	return err
}

func (s *redisChannel) Close() error {
	s.once.Do(func() {
		if err := s.pubSub.Unsubscribe(context.Background(), s.readChannel); err != nil {
			logger.Errorf("Redis unsubscribe channel %s err: %s", s.readChannel, err)
		}
		s.errMsg = s.pubSub.Close()
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

func (s *redisChannel) HandleRoomEvent(event string, msg *RoomMessage) {
	err := s.sendMessage(msg)
	if err != nil {
		logger.Errorf("Redis send event room %s err: %s", s.roomId, err)
	}
}
