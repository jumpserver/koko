package exchange

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"time"

	uuid "github.com/satori/go.uuid"

	"github.com/jumpserver/koko/pkg/logger"
	"github.com/jumpserver/koko/pkg/model"
	"github.com/mediocregopher/radix/v3"
)

const (
	globalRoomsKey = "JUMPSERVER:KOKO:ROOMS"

	eventsChannel = "JUMPSERVER:KOKO:EVENTS:CHANNEL"

	resultsChannel = "JUMPSERVER:KOKO:EVENTS:RESULT"

	sessionsChannelPrefix = "JMS:KOKO:SESSIONS:"
)

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

func newRedisManager(cfg Config) (*redisRoomManager, error) {
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

	pubSub, err := radix.PersistentPubSubWithOpts("", "",
		radix.PersistentPubSubConnFunc(connFunc))
	if err != nil {
		return nil, err
	}
	redisMsgCh := make(chan radix.PubSubMessage)
	err = pubSub.Subscribe(redisMsgCh, eventsChannel, resultsChannel)
	if err != nil {
		return nil, err
	}
	pool, err := radix.NewPool("", "", cfg.MaxActive, radix.PoolConnFunc(connFunc))
	if err != nil {
		return nil, err
	}

	m := &redisRoomManager{
		Id:                     uuid.NewV4().String(),
		pool:                   pool,
		connFunc:               connFunc,
		localRoomCache:         newLocalCache(),
		remoteRoomCache:        newLocalCache(),
		pubSub:                 pubSub,
		subscribeEventsMsgCh:   redisMsgCh,
		reqChan:                make(chan *subscribeRequest),
		reqCancelChan:          make(chan *subscribeRequest),
		removeProxyRoomChan:    make(chan *Room),
		responseChan:           make(chan chan *subscribeResponse),
		removeRedisUserConChan: make(chan *redisChannel),
	}
	go m.run()
	return m, nil
}

type redisRoomManager struct {
	Id              string
	pool            *radix.Pool
	connFunc        radix.ConnFunc
	localRoomCache  *localCache
	remoteRoomCache *localCache

	subscribeEventsMsgCh chan radix.PubSubMessage
	pubSub               radix.PubSubConn

	responseChan chan chan *subscribeResponse

	reqChan chan *subscribeRequest

	reqCancelChan chan *subscribeRequest

	removeRedisUserConChan chan *redisChannel

	removeProxyRoomChan chan *Room
}

func (m *redisRoomManager) Add(s *Room) {
	m.localRoomCache.Add(s)
	m.storeRoomId(s.Id)
}

func (m *redisRoomManager) Delete(s *Room) {
	m.localRoomCache.Delete(s)
	m.removeRoomId(s.Id)
}

func (m *redisRoomManager) Get(sid string) *Room {
	if r := m.localRoomCache.Get(sid); r != nil {
		return r
	}
	if ok := m.checkRoomExist(sid); ok {
		return m.getRemoteSessionRoom(sid)
	}
	return nil
}

func (m *redisRoomManager) checkRoomExist(roomId string) bool {
	var countInt int
	err := m.pool.Do(radix.Cmd(&countInt, "SISMEMBER", globalRoomsKey, roomId))
	if err != nil {
		logger.Errorf("Redis cache check room err: %s", roomId, err)
		return false
	}
	return countInt == 1
}

// 全局 加入room
func (m *redisRoomManager) storeRoomId(roomId string) {
	err := m.pool.Do(radix.Cmd(nil, "SADD", globalRoomsKey, roomId))
	if err != nil {
		logger.Errorf("Redis Cache store room %s err: %s", roomId, err)
		return
	}
	logger.Debugf("Redis Cache store room %s success", roomId)
}

// 全局 删除room
func (m *redisRoomManager) removeRoomId(roomId string) {
	err := m.pool.Do(radix.Cmd(nil, "SREM", globalRoomsKey, roomId))
	if err != nil {
		logger.Errorf("Redis cache remove room %s err: %s", roomId, err)
	} else {
		logger.Debugf("Redis cache remove room %s success", roomId)
	}
	// 发布退出事件
	req := m.createRoomEventRequest(roomId, model.ExitEvent)
	_, err = m.sendRequest(&req)
	if err != nil {
		logger.Errorf("Redis cache publish room %s exit event err: %s", roomId, err)
	} else {
		logger.Debugf("Redis cache publish room %s exit event success", roomId)
	}
}

func (m *redisRoomManager) publishCommand(channel string, p []byte) error {
	cmd := radix.FlatCmd(nil, "PUBLISH", channel, p)
	return m.pool.Do(cmd)
}

func (m *redisRoomManager) run() {

	requestsMap := make(map[string]chan *subscribeResponse)

	// 本地 Room 增加 redisCon，key 是 room id
	redisUserCons := make(map[string]*redisChannel)

	for {
		select {
		case req := <-m.reqChan:
			responseChan := make(chan *subscribeResponse, 1)
			m.responseChan <- responseChan
			switch req.Event {
			case model.JoinEvent:
				//	校验本地 是否已经存在
				if room := m.remoteRoomCache.Get(req.RoomId); room != nil {
					logger.Debugf("Redis cache already create room %s", req.RoomId)
					responseChan <- &subscribeResponse{
						Req:  req,
						room: room,
						err:  nil,
					}
					continue
				}
				// 本地不存在则发送请求信号
				if err := m.publishRequest(req); err != nil {
					logger.Debugf("Redis cache send request join room %s err: %s", req.RoomId, err)
					responseChan <- &subscribeResponse{
						Req:  req,
						room: nil,
						err:  err,
					}
					continue
				}
				requestsMap[req.ReqId] = responseChan //不阻塞 等待返回结果
			case model.ExitEvent:
				if err := m.publishRequest(req); err != nil {
					responseChan <- &subscribeResponse{
						Req: req,
						err: err,
					}
					delete(requestsMap, req.ReqId)
					logger.Errorf("Redis cache send request %s event %s err: %s", req.ReqId, req.Event, err)
					continue
				}
				responseChan <- &subscribeResponse{Req: req}
			default:

			}
			logger.Debugf("Redis cache send event %s for room %s", req.Event, req.RoomId)

		case req := <-m.reqCancelChan:
			delete(requestsMap, req.ReqId)
			logger.Debugf("Redis cache cancel request %s", req.ReqId)

		case redisUserCon := <-m.removeRedisUserConChan:
			delete(redisUserCons, redisUserCon.roomId)

		case room := <-m.removeProxyRoomChan:
			cacheRoom := m.remoteRoomCache.Get(room.Id)
			if cacheRoom == nil {
				continue
			}
			logger.Infof("Redis cache delete remote room %s", room.Id)
			m.remoteRoomCache.Delete(room)
			req := m.createRoomEventRequest(room.Id, model.LeaveEvent)
			if err := m.publishRequest(&req); err != nil {
				logger.Errorf("Redis cache send leave event for room %s err: %s", room.Id, err)
			} else {
				logger.Debugf("Redis cache send leave event for room %s success", room.Id)
			}

		case redisMsg := <-m.subscribeEventsMsgCh:
			var req subscribeRequest
			if err := json.Unmarshal(redisMsg.Message, &req); err != nil {
				logger.Errorf("Redis cache unmarshal request msg err: %s", err)
				continue
			}

			switch redisMsg.Channel {
			case resultsChannel:
				switch req.Event {
				case model.JoinSuccessEvent:
					responseChan, ok := requestsMap[req.ReqId]
					if !ok {
						logger.Debugf("Redis cache ignore not self result request %s", req.ReqId)
						continue
					}
					logger.Infof("Redis cache request %s receive result", req.ReqId)
					// 请求结束，移除缓存, 返回请求的结果
					delete(requestsMap, req.ReqId)

					var res subscribeResponse
					res.Req = &req

					redisCon, err := m.connFunc("", "")
					if err != nil {
						logger.Errorf("Redis cache request %s create redis conn err: %s", req.ReqId, err)
						res.err = err
						responseChan <- &res
						continue
					}

					pubSub := radix.PubSub(redisCon)
					redisMsgCh := make(chan radix.PubSubMessage)
					writeChannel := createSessionChannel(fmt.Sprintf("%s.write", req.RoomId))
					readChannel := createSessionChannel(fmt.Sprintf("%s.read", req.RoomId))
					if err = pubSub.Subscribe(redisMsgCh, readChannel); err != nil {
						_ = pubSub.Close()
						logger.Errorf("Redis cache request %s subscribe channel err: %s", req.ReqId, err)
						res.err = err
						responseChan <- &res
						continue
					}
					userInputChan := make(chan *model.RoomMessage)
					room := CreateRoom(req.RoomId, userInputChan)
					m.remoteRoomCache.Add(room)
					s := &redisChannel{
						roomId:       req.RoomId,
						writeChannel: writeChannel,
						readChannel:  readChannel,
						pubSub:       pubSub,
						subMsgCh:     redisMsgCh,
						manager:      m,
						done:         make(chan struct{}),
						count:        make(chan int),
					}
					go proxyRoom(room, s, userInputChan)
					res.room = room
					responseChan <- &res // 容量为1， 不阻塞
					logger.Infof("Redis cache request %s finished", req.ReqId)
				default:
					logger.Infof("Result channel receive unhandled event %s", req.Event)
				}

			case eventsChannel:
				switch req.Event {
				case model.JoinEvent:
					/*
						1. 检查是否是自己创建的req: 是则忽略
						2. 检查是否已经创建过redisUserCon: 是则发送JoinSuccessEvent
						3. 检查是否是本KOKO创建的Session会话: 是则创建redisUserCon，并发送JoinSuccessEvent
					*/

					if _, ok := requestsMap[req.ReqId]; ok {
						logger.Debugf("Redis cache ignore self request %s", req.ReqId)
						continue
					}
					// 创建result channel的req
					successReq := m.createRoomResultRequest(req.ReqId,
						req.RoomId, model.JoinSuccessEvent)

					// 本地是否已经创建过 redisUserCons
					if srv, ok := redisUserCons[req.RoomId]; ok {
						logger.Infof("Redis cache already create redis con for room %s", req.RoomId)
						if err := m.publishRequest(&successReq); err != nil {
							logger.Errorf("Redis cache reply request %s join event err %s", req.ReqId, err)
						} else {
							logger.Infof("Redis cache reply request %s join event", req.ReqId)
							//  统计一下 req的 count
							srv.addSubscribeCount(1)
						}
						continue
					}

					// 如果是当前节点 KoKo 创建的session
					if r := m.localRoomCache.Get(req.RoomId); r != nil {
						redisCon, err := m.connFunc("", "")
						if err != nil {
							logger.Errorf("Redis cache create redis conn for request %s err %s", req.ReqId, err)
							continue
						}
						pubSub := radix.PubSub(redisCon)
						subMsgCh := make(chan radix.PubSubMessage)
						writeChannel := createSessionChannel(fmt.Sprintf("%s.read", req.RoomId))
						readChannel := createSessionChannel(fmt.Sprintf("%s.write", req.RoomId))
						if err = pubSub.Subscribe(subMsgCh, readChannel); err != nil {
							_ = pubSub.Close()
							logger.Errorf("Redis cache create pubSub conn for request %s err: %s", req.ReqId, err)
							continue
						}

						s := &redisChannel{
							roomId:       req.RoomId,
							writeChannel: writeChannel,
							readChannel:  readChannel,
							pubSub:       pubSub,
							subMsgCh:     subMsgCh,
							manager:      m,
							done:         make(chan struct{}),
							count:        make(chan int),
						}

						redisUserCons[req.RoomId] = s
						go proxyUserCon(r, s)
						if err := m.publishRequest(&successReq); err != nil {
							logger.Errorf("Redis cache reply request %s join event err %s", req.ReqId, err)
						} else {
							logger.Infof("Redis cache reply request %s join event", req.ReqId)
						}
						continue
					}
					logger.Infof("The current KoKo node has no session room %s", req.RoomId)
					// 非本节点 koko 创建的session
				case model.LeaveEvent:
					if srv, ok := redisUserCons[req.RoomId]; ok {
						srv.addSubscribeCount(-1)
						logger.Infof("Event channel receive room %s leave event", req.RoomId)
					}

				case model.ExitEvent:
					if room := m.remoteRoomCache.Get(req.RoomId); room != nil {
						logger.Infof("Event channel receive room %s exit", req.RoomId)
						m.remoteRoomCache.Delete(room)
					}
				default:
					logger.Infof("Event channel receive unhandled event %s: %v", req.Event, req)
				}

			}
		}
	}
}

func (m *redisRoomManager) getRemoteSessionRoom(roomId string) *Room {
	logger.Infof("Waiting subscribe remote room %s result", roomId)

	req := m.createRoomEventRequest(roomId, model.JoinEvent)
	res, err := m.sendJoinRequest(&req)
	if err != nil {
		logger.Errorf("get remote session room err: %s", err)
		return nil
	}
	return res.room
}

func (m *redisRoomManager) uniqueReqId(sid string) string {
	return fmt.Sprintf("%d:%s:%s", time.Now().Unix(), m.Id, sid)
}

func (m *redisRoomManager) sendJoinRequest(req *subscribeRequest) (*subscribeResponse, error) {
	return m.sendRequest(req)
}

func (m *redisRoomManager) sendRequest(req *subscribeRequest) (*subscribeResponse, error) {
	ctx, cancelFunc := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelFunc()
	m.reqChan <- req
	resultChan := <-m.responseChan
	select {
	case <-ctx.Done():
		select {
		case m.reqCancelChan <- req:

		case res := <-resultChan:
			return res, res.err
		}
	case res := <-resultChan:
		return res, res.err
	}
	return nil, fmt.Errorf("Redis cache send request event %s time out ", req.Event)
}

func (m *redisRoomManager) publishRequest(req *subscribeRequest) error {
	body, _ := json.Marshal(req)
	return m.publishCommand(req.Channel, body)
}

func (m *redisRoomManager) createRoomEventRequest(roomId, event string) subscribeRequest {
	return subscribeRequest{
		ReqId:   m.uniqueReqId(roomId),
		RoomId:  roomId,
		Event:   event,
		Channel: eventsChannel,
	}
}

func (m *redisRoomManager) createRoomResultRequest(reqId, roomId, event string) subscribeRequest {
	return subscribeRequest{
		ReqId:   reqId,
		RoomId:  roomId,
		Event:   event,
		Channel: resultsChannel,
	}
}

type subscribeResponse struct {
	Req  *subscribeRequest
	room *Room
	err  error
}

type subscribeRequest struct {
	ReqId   string `json:"req_id"` //
	RoomId  string `json:"room_id"`
	Event   string `json:"event"`
	Channel string `json:"-"`
}

func createSessionChannel(channel string) string {
	return fmt.Sprintf("%s%s", sessionsChannelPrefix, channel)
}
