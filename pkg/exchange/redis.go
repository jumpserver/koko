package exchange

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"sync"
	"time"

	"github.com/jumpserver/koko/pkg/logger"
	"github.com/jumpserver/koko/pkg/model"

	"github.com/mediocregopher/radix/v3"
)

const globalRoomsKey = "JumpServer.KoKo.Share.Rooms"

type Config struct {
	// Defaults to "tcp".
	Network string
	// Addr of a single redis server instance.
	// See "Clusters" field for clusters support.
	// Defaults to "127.0.0.1:6379".
	Addr string
	// Clusters a list of network addresses for clusters.
	// If not empty "Addr" is ignored.
	Clusters []string

	Password    string
	DialTimeout time.Duration

	// MaxActive defines the size connection pool.
	// Defaults to 10.
	MaxActive int

	DBIndex uint64
}

func NewRedisExchange(cfg Config) (*redisExchange, error) {
	if cfg.Network == "" {
		cfg.Network = "tcp"
	}

	if cfg.Addr == "" && len(cfg.Clusters) == 0 {
		cfg.Addr = "127.0.0.1:6379"
	}

	if cfg.DialTimeout < 0 {
		cfg.DialTimeout = 30 * time.Second
	}

	if cfg.MaxActive == 0 {
		cfg.MaxActive = 10
	}

	var dialOptions []radix.DialOpt

	if cfg.Password != "" {
		dialOptions = append(dialOptions, radix.DialAuthPass(cfg.Password))
	}

	if cfg.DialTimeout > 0 {
		dialOptions = append(dialOptions, radix.DialTimeout(cfg.DialTimeout))
	}

	if cfg.DBIndex != 0 {
		dialOptions = append(dialOptions, radix.DialSelectDB(int(cfg.DBIndex)))
	}

	var connFunc radix.ConnFunc

	if len(cfg.Clusters) > 0 {
		cluster, err := radix.NewCluster(cfg.Clusters)
		if err != nil {
			// maybe an
			// ERR This instance has cluster support disabled
			return nil, err
		}

		connFunc = func(network, addr string) (radix.Conn, error) {
			topo := cluster.Topo()
			node := topo[rand.Intn(len(topo))]
			return radix.Dial(cfg.Network, node.Addr, dialOptions...)
		}
	} else {
		connFunc = func(network, addr string) (radix.Conn, error) {
			return radix.Dial(cfg.Network, cfg.Addr, dialOptions...)
		}
	}

	pool, err := radix.NewPool("", "", cfg.MaxActive, radix.PoolConnFunc(connFunc))
	if err != nil {
		return nil, err
	}

	exc := &redisExchange{
		pool:         pool,
		connFunc:     connFunc,
		createdRooms: make(map[string]*redisRoom),
		joinedRooms:  make(map[string]map[*redisRoom]struct{}),
	}
	return exc, nil
}

type redisExchange struct {
	pool         *radix.Pool
	connFunc     radix.ConnFunc
	createdRooms map[string]*redisRoom
	joinedRooms  map[string]map[*redisRoom]struct{}
	mu           sync.Mutex
}

func (exc *redisExchange) checkRoomExist(roomId string) bool {
	var countInt int
	err := exc.pool.Do(radix.Cmd(&countInt, "SISMEMBER", globalRoomsKey, roomId))
	if err != nil {
		logger.Error(err)
		return false
	}
	return countInt == 1
}

// 全局 加入room
func (exc *redisExchange) storeRoomId(roomId string) {
	err := exc.pool.Do(radix.Cmd(nil, "SADD", globalRoomsKey, roomId))
	if err != nil {
		logger.Error(err)
	}
}

// 全局 删除room
func (exc *redisExchange) removeRoomId(roomId string) {
	err := exc.pool.Do(radix.Cmd(nil, "SREM", globalRoomsKey, roomId))
	if err != nil {
		logger.Error(err)
	}
}

func (exc *redisExchange) publishCommand(channel string, p []byte) error {
	cmd := radix.FlatCmd(nil, "PUBLISH", channel, p)
	return exc.pool.Do(cmd)
}

func (exc *redisExchange) JoinRoom(receiveChan chan<- model.RoomMessage, roomId string) (Room, error) {
	exc.mu.Lock()
	defer exc.mu.Unlock()
	if !exc.checkRoomExist(roomId) {
		return nil, fmt.Errorf("no redisRoom")
	}
	redisMsgCh := make(chan radix.PubSubMessage)
	go func() {
		for redisMsg := range redisMsgCh {
			_ = redisMsg.Message
			var msg model.RoomMessage
			_ = json.Unmarshal(redisMsg.Message, &msg)
			receiveChan <- msg
		}
		logger.Infof("Joined Room %s stop receive message", roomId)
	}()

	pubSub := radix.PersistentPubSub("", "", exc.connFunc)
	writeChannel := fmt.Sprintf("%s.write", roomId)
	readChannel := fmt.Sprintf("%s.read", roomId)
	s := &redisRoom{
		roomId:       roomId,
		writeChannel: writeChannel,
		readChannel:  readChannel,
		pubSub:       pubSub,
		redisMsgCh:   redisMsgCh,
		exc:          exc,
		messageChan:  receiveChan,
	}
	_ = pubSub.Subscribe(redisMsgCh, readChannel)
	if parties, ok := exc.joinedRooms[roomId]; ok {
		parties[s] = struct{}{}
	} else {
		exc.joinedRooms[roomId] = map[*redisRoom]struct{}{
			s: {},
		}
	}
	return s, nil
}

func (exc *redisExchange) LeaveRoom(exRoom Room, roomId string) {
	sub, ok := exRoom.(*redisRoom)
	if !ok {
		return
	}
	exc.mu.Lock()
	defer exc.mu.Unlock()
	if parties, ok := exc.joinedRooms[roomId]; ok {
		delete(parties, sub)
	}
	if err := sub.pubSub.Unsubscribe(sub.redisMsgCh, sub.readChannel); err != nil {
		logger.Errorf("Redis leave room unsubscribe err: %s", err)
	}
	if err := sub.pubSub.Close(); err != nil {
		logger.Errorf("Redis leave room pubSub close err: %s", err)
	}
	close(sub.redisMsgCh)
	close(sub.messageChan)
}

func (exc *redisExchange) DestroyRoom(exRoom Room) {
	sub, ok := exRoom.(*redisRoom)
	if !ok {
		return
	}
	exc.removeRoomId(sub.roomId)
	exc.mu.Lock()
	defer exc.mu.Unlock()
	delete(exc.createdRooms, sub.roomId)
	if err := sub.pubSub.Unsubscribe(sub.redisMsgCh, sub.readChannel); err != nil {
		logger.Errorf("Redis destroy room unsubscribe err: %s", err)
	}
	if err := sub.pubSub.Close(); err != nil {
		logger.Errorf("Redis destroy room pubSub close err: %s", err)
	}
	close(sub.redisMsgCh)
	close(sub.messageChan)

}

func (exc *redisExchange) CreateRoom(receiveChan chan<- model.RoomMessage, roomId string) Room {
	exc.mu.Lock()
	defer exc.mu.Unlock()
	redisMsgCh := make(chan radix.PubSubMessage)
	go func() {
		for redisMsg := range redisMsgCh {
			var msg model.RoomMessage
			_ = json.Unmarshal(redisMsg.Message, &msg)
			receiveChan <- msg
		}
		logger.Infof("Redis room %s stop receive message", roomId)
	}()

	pubSub := radix.PersistentPubSub("", "", exc.connFunc)
	writeChannel := fmt.Sprintf("%s.read", roomId)
	readChannel := fmt.Sprintf("%s.write", roomId)
	r := &redisRoom{
		roomId:       roomId,
		writeChannel: writeChannel,
		readChannel:  readChannel,
		pubSub:       pubSub,
		redisMsgCh:   redisMsgCh,
		exc:          exc,
		messageChan:  receiveChan,
	}

	_ = pubSub.Subscribe(redisMsgCh, readChannel)
	exc.createdRooms[roomId] = r
	exc.storeRoomId(roomId)
	return r
}

func (exc *redisExchange) Close() {
	exc.mu.Lock()
	for roomID, createdRoom := range exc.createdRooms {
		exc.removeRoomId(roomID)
		close(createdRoom.messageChan)
	}
	defer exc.mu.Unlock()
	for _, parties := range exc.joinedRooms {
		for party := range parties {
			_ = party.pubSub.Close()
			close(party.redisMsgCh)
			close(party.messageChan)
		}
	}
}

type redisRoom struct {
	roomId       string
	writeChannel string
	readChannel  string
	pubSub       radix.PubSubConn
	redisMsgCh   chan radix.PubSubMessage
	exc          *redisExchange
	messageChan  chan<- model.RoomMessage
}

func (s *redisRoom) Publish(msg model.RoomMessage) {
	err := s.exc.publishCommand(s.writeChannel, msg.Marshal())
	if err != nil {
		logger.Errorf("Redis publish message err: %s", err)
	}
}
