package exchange

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/go-redis/redis/v8"

	"github.com/jumpserver/koko/pkg/common"
	"github.com/jumpserver/koko/pkg/logger"
)

const (
	globalRoomsKey = "JUMPSERVER:KOKO:ROOMS"
	roomOwnersKey  = "JUMPSERVER:KOKO:ROOM:OWNERS"
	managerKeyBase = "JUMPSERVER:KOKO:MANAGER:"

	eventsChannel = "JUMPSERVER:KOKO:EVENTS:CHANNEL"

	resultsChannel = "JUMPSERVER:KOKO:EVENTS:RESULT"

	sessionsChannelPrefix = "JMS:KOKO:SESSIONS:"

	redisRequestTimeout = 10 * time.Second
	redisCommandTimeout = 10 * time.Second
	redisCloseTimeout   = 5 * time.Second
	managerLeaseTTL     = 90 * time.Second
	managerHeartbeat    = 30 * time.Second
)

var errRedisManagerClosed = errors.New("redis room manager closed")

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

	DBIndex int

	SentinelsHost    string
	SentinelPassword string
	SSLCa            string
	SSLCert          string
	SSLKey           string
	UseSSL           bool
}

func newRedisManager(cfg Config) (*redisRoomManager, error) {
	if cfg.Network == "" {
		cfg.Network = "tcp"
	}

	if cfg.Addr == "" && len(cfg.Clusters) == 0 {
		cfg.Addr = "127.0.0.1:6379"
	}

	if cfg.DialTimeout <= 0 {
		cfg.DialTimeout = 30 * time.Second
	}

	if cfg.MaxActive == 0 {
		cfg.MaxActive = 10
	}

	var tlsCfg *tls.Config
	if cfg.UseSSL {
		tlsConfig := tls.Config{MinVersion: tls.VersionTLS12}
		if cfg.SSLCert != "" && cfg.SSLKey != "" {
			cert, err := tls.LoadX509KeyPair(cfg.SSLCert, cfg.SSLKey)
			if err != nil {
				return nil, err
			}
			logger.Debugf("Load redis SSL cert: %s, key: %s", cfg.SSLCert, cfg.SSLKey)
			tlsConfig.Certificates = []tls.Certificate{cert}
		}
		if cfg.SSLCa != "" {
			certPool := x509.NewCertPool()
			buf, err := os.ReadFile(cfg.SSLCa)
			if err != nil {
				return nil, err
			}
			logger.Debugf("Load redis SSL ca: %s", cfg.SSLCa)
			if ok := certPool.AppendCertsFromPEM(buf); !ok {
				return nil, fmt.Errorf("invalid Redis SSL CA: %s", cfg.SSLCa)
			}
			tlsConfig.RootCAs = certPool
		}
		tlsCfg = &tlsConfig
	}

	var client redis.UniversalClient
	if len(cfg.Clusters) > 0 {
		if cfg.DBIndex != 0 {
			return nil, fmt.Errorf("Redis cluster mode supports only database 0")
		}
		client = redis.NewClusterClient(&redis.ClusterOptions{
			Addrs:        cfg.Clusters,
			Password:     cfg.Password,
			DialTimeout:  cfg.DialTimeout,
			ReadTimeout:  redisCommandTimeout,
			WriteTimeout: redisCommandTimeout,
			PoolSize:     cfg.MaxActive,
			MaxRetries:   -1,
			TLSConfig:    tlsCfg,
			ClusterSlots: common.RedisClusterSlots(cfg.Clusters, redis.Options{
				Password:     cfg.Password,
				DialTimeout:  cfg.DialTimeout,
				ReadTimeout:  redisCommandTimeout,
				WriteTimeout: redisCommandTimeout,
				PoolSize:     1,
				MaxRetries:   -1,
				TLSConfig:    tlsCfg,
			}),
		})
	} else if cfg.SentinelsHost != "" {
		sentinels := strings.SplitN(cfg.SentinelsHost, "/", 2)
		if len(sentinels) != 2 {
			return nil, fmt.Errorf("invalid sentinel host: %s", cfg.SentinelsHost)
		}
		sentinelServiceName := sentinels[0]
		sentinelHosts := strings.Split(sentinels[1], ",")
		client = redis.NewFailoverClient(&redis.FailoverOptions{
			MasterName:       sentinelServiceName,
			SentinelAddrs:    sentinelHosts,
			SentinelPassword: cfg.SentinelPassword,
			Password:         cfg.Password,
			DB:               cfg.DBIndex,
			DialTimeout:      cfg.DialTimeout,
			ReadTimeout:      redisCommandTimeout,
			WriteTimeout:     redisCommandTimeout,
			PoolSize:         cfg.MaxActive,
			MaxRetries:       -1,
			TLSConfig:        tlsCfg,
		})
	} else {
		client = redis.NewClient(&redis.Options{
			Network:      cfg.Network,
			Addr:         cfg.Addr,
			Password:     cfg.Password,
			DB:           cfg.DBIndex,
			DialTimeout:  cfg.DialTimeout,
			ReadTimeout:  redisCommandTimeout,
			WriteTimeout: redisCommandTimeout,
			PoolSize:     cfg.MaxActive,
			MaxRetries:   -1,
			TLSConfig:    tlsCfg,
		})
	}

	ctx, cancel := context.WithTimeout(context.Background(), cfg.DialTimeout)
	defer cancel()
	if err := client.Ping(ctx).Err(); err != nil {
		_ = client.Close()
		return nil, err
	}
	pubSub, redisMsgCh, err := subscribeRedisChannels(
		client, cfg.DialTimeout, eventsChannel, resultsChannel,
	)
	if err != nil {
		_ = client.Close()
		logger.Errorf("Redis pubSub err: %s", err)
		return nil, err
	}

	subscribeTimeout := min(cfg.DialTimeout, redisCommandTimeout)
	m := &redisRoomManager{
		Id:                     common.UUID(),
		client:                 client,
		subscribeTimeout:       subscribeTimeout,
		localRoomCache:         newLocalCache(),
		remoteRoomCache:        newLocalCache(),
		pubSub:                 pubSub,
		subscribeEventsMsgCh:   redisMsgCh,
		reqChan:                make(chan *managerRequest),
		reqCancelChan:          make(chan string, 32),
		removeProxyRoomChan:    make(chan *Room, 32),
		removeRedisUserConChan: make(chan *redisChannel, 32),
		done:                   make(chan struct{}),
		joining:                make(map[string]*joinCall),
	}
	leaseCtx, leaseCancel := context.WithTimeout(context.Background(), redisCommandTimeout)
	err = m.refreshLease(leaseCtx)
	leaseCancel()
	if err != nil {
		_ = pubSub.Close()
		_ = client.Close()
		return nil, err
	}
	go m.run()
	return m, nil
}

func subscribeRedisChannels(
	client redis.UniversalClient, timeout time.Duration, channels ...string,
) (*redis.PubSub, <-chan *redis.Message, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	pubSub := client.Subscribe(ctx, channels...)
	for range channels {
		if _, err := pubSub.Receive(ctx); err != nil {
			_ = pubSub.Close()
			return nil, nil, err
		}
	}
	return pubSub, pubSub.Channel(), nil
}

type redisRoomManager struct {
	Id               string
	client           redis.UniversalClient
	subscribeTimeout time.Duration
	localRoomCache   *localCache
	remoteRoomCache  *localCache

	subscribeEventsMsgCh <-chan *redis.Message
	pubSub               *redis.PubSub

	reqChan chan *managerRequest

	reqCancelChan chan string

	removeRedisUserConChan chan *redisChannel

	removeProxyRoomChan chan *Room

	done           chan struct{}
	doneOnce       sync.Once
	closeOnce      sync.Once
	closeErr       error
	joinMu         sync.Mutex
	joining        map[string]*joinCall
	roomScanCursor uint64
}

type managerRequest struct {
	req      *subscribeRequest
	response chan *subscribeResponse
	done     <-chan struct{}
}

type joinCall struct {
	done chan struct{}
	room *Room
}

type redisUserConState struct {
	channel     *redisChannel
	subscribers map[string]struct{}
	legacyCount int
}

func (s *redisUserConState) add(req *subscribeRequest) bool {
	if req.ManagerId == "" {
		s.legacyCount++
		return true
	}
	if _, ok := s.subscribers[req.ManagerId]; ok {
		return false
	}
	s.subscribers[req.ManagerId] = struct{}{}
	return true
}

func (s *redisUserConState) remove(req *subscribeRequest) bool {
	if req.ManagerId == "" {
		if s.legacyCount == 0 {
			return false
		}
		s.legacyCount--
		return true
	}
	if _, ok := s.subscribers[req.ManagerId]; !ok {
		return false
	}
	delete(s.subscribers, req.ManagerId)
	return true
}

func (s *redisUserConState) count() int {
	return len(s.subscribers) + s.legacyCount
}

func closeRedisUserCon(redisUserCons map[string]*redisUserConState, roomId string, state *redisUserConState) {
	if redisUserCons[roomId] != state {
		return
	}
	delete(redisUserCons, roomId)
	if err := state.channel.Close(); err != nil {
		logger.Errorf("Redis channel close err: %s", err)
	}
}

func (m *redisRoomManager) removeExpiredSubscribers(
	ctx context.Context, redisUserCons map[string]*redisUserConState,
) error {
	managers := make(map[string]*redis.IntCmd)
	pipe := m.client.Pipeline()
	for _, state := range redisUserCons {
		for managerId := range state.subscribers {
			if managers[managerId] == nil {
				managers[managerId] = pipe.Exists(ctx, managerLeaseKey(managerId))
			}
		}
	}
	if len(managers) == 0 {
		_ = pipe.Close()
		return nil
	}
	if _, err := pipe.Exec(ctx); err != nil {
		_ = pipe.Close()
		return err
	}
	_ = pipe.Close()
	for roomId, state := range redisUserCons {
		for managerId := range state.subscribers {
			if managers[managerId].Val() == 0 {
				delete(state.subscribers, managerId)
			}
		}
		if state.count() == 0 {
			closeRedisUserCon(redisUserCons, roomId, state)
		}
	}
	return nil
}

func (m *redisRoomManager) cleanupStaleRooms(ctx context.Context) error {
	rooms, cursor, err := m.client.HScan(ctx, roomOwnersKey, m.roomScanCursor, "", 100).Result()
	if err != nil {
		return err
	}
	m.roomScanCursor = cursor
	managers := make(map[string]*redis.IntCmd)
	pipe := m.client.Pipeline()
	for i := 1; i < len(rooms); i += 2 {
		managerId := rooms[i]
		if managers[managerId] == nil {
			managers[managerId] = pipe.Exists(ctx, managerLeaseKey(managerId))
		}
	}
	if len(managers) == 0 {
		_ = pipe.Close()
		return nil
	}
	if _, err := pipe.Exec(ctx); err != nil {
		_ = pipe.Close()
		return err
	}
	_ = pipe.Close()
	for i := 0; i+1 < len(rooms); i += 2 {
		roomId, managerId := rooms[i], rooms[i+1]
		if managers[managerId].Val() > 0 {
			continue
		}
		if err := m.removeStaleRoom(ctx, roomId, managerId); err != nil {
			return err
		}
	}
	return nil
}

func (m *redisRoomManager) Add(s *Room) {
	if _, added := m.localRoomCache.AddIfAbsent(s); !added {
		return
	}
	m.storeRoomId(s.Id)
}

func (m *redisRoomManager) Delete(s *Room) {
	if !m.localRoomCache.DeleteIf(s) {
		return
	}
	m.removeRoomId(s.Id)
}

func (m *redisRoomManager) Get(sid string) *Room {
	if r := m.localRoomCache.Get(sid); r != nil {
		return r
	}
	if r := m.remoteRoomCache.Get(sid); r != nil {
		return r
	}
	if ok := m.checkRoomExist(sid); ok {
		return m.getRemoteSessionRoom(sid)
	}
	return nil
}

func (m *redisRoomManager) checkRoomExist(roomId string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), redisCommandTimeout)
	defer cancel()
	exists, err := m.client.SIsMember(ctx, globalRoomsKey, roomId).Result()
	if err != nil {
		logger.Errorf("Redis cache check room %s err: %s", roomId, err)
		return false
	}
	if !exists {
		return false
	}
	owner, err := m.client.HGet(ctx, roomOwnersKey, roomId).Result()
	if errors.Is(err, redis.Nil) {
		return true
	}
	if err != nil {
		logger.Errorf("Redis cache check room %s owner err: %s", roomId, err)
		return true
	}
	alive, err := m.client.Exists(ctx, managerLeaseKey(owner)).Result()
	if err != nil {
		logger.Errorf("Redis cache check room %s owner lease err: %s", roomId, err)
		return true
	}
	if alive > 0 {
		return true
	}
	if err := m.removeStaleRoom(ctx, roomId, owner); err != nil {
		logger.Errorf("Redis cache remove stale room %s err: %s", roomId, err)
	}
	return false
}

// 全局 加入room
func (m *redisRoomManager) storeRoomId(roomId string) {
	ctx, cancel := context.WithTimeout(context.Background(), redisCommandTimeout)
	defer cancel()
	if err := m.refreshLease(ctx); err != nil {
		logger.Errorf("Redis cache refresh manager lease err: %s", err)
		return
	}
	if err := m.client.HSet(ctx, roomOwnersKey, roomId, m.Id).Err(); err != nil {
		logger.Errorf("Redis cache store room %s owner err: %s", roomId, err)
		return
	}
	err := m.client.SAdd(ctx, globalRoomsKey, roomId).Err()
	if err != nil {
		_, _ = m.removeRoomOwner(ctx, roomId)
		logger.Errorf("Redis Cache store room %s err: %s", roomId, err)
		return
	}
	logger.Debugf("Redis Cache store room %s success", roomId)
}

// 全局 删除room
func (m *redisRoomManager) removeRoomId(roomId string) {
	ctx, cancel := context.WithTimeout(context.Background(), redisCommandTimeout)
	defer cancel()
	if err := m.client.SRem(ctx, globalRoomsKey, roomId).Err(); err != nil {
		logger.Errorf("Redis cache remove room %s err: %s", roomId, err)
		return
	}
	removed, err := m.removeRoomOwner(ctx, roomId)
	if err != nil {
		logger.Errorf("Redis cache remove room %s err: %s", roomId, err)
		_ = m.client.SAdd(ctx, globalRoomsKey, roomId).Err()
		return
	}
	if !removed {
		logger.Debugf("Redis cache ignore room %s owned by another manager", roomId)
		if owner, getErr := m.client.HGet(ctx, roomOwnersKey, roomId).Result(); getErr == nil && owner != "" {
			_ = m.client.SAdd(ctx, globalRoomsKey, roomId).Err()
		}
		return
	}
	logger.Debugf("Redis cache remove room %s success", roomId)
	// 发布退出事件
	req := m.createRoomEventRequest(roomId, ExitEvent)
	_, err = m.sendRequest(&req)
	if err != nil {
		logger.Errorf("Redis cache publish room %s exit event err: %s", roomId, err)
	} else {
		logger.Debugf("Redis cache publish room %s exit event success", roomId)
	}
}

func managerLeaseKey(managerId string) string {
	return managerKeyBase + managerId
}

func (m *redisRoomManager) refreshLease(ctx context.Context) error {
	return m.client.Set(ctx, managerLeaseKey(m.Id), m.Id, managerLeaseTTL).Err()
}

func (m *redisRoomManager) renewLease(ctx context.Context) (bool, error) {
	exists, err := m.client.Exists(ctx, managerLeaseKey(m.Id)).Result()
	if err != nil {
		return false, err
	}
	if err := m.refreshLease(ctx); err != nil {
		return false, err
	}
	return exists == 0, nil
}

func (m *redisRoomManager) restoreLocalRooms(ctx context.Context) error {
	rooms := m.localRoomCache.Rooms()
	if len(rooms) == 0 {
		return nil
	}
	owners := make([]interface{}, 0, len(rooms)*2)
	members := make([]interface{}, 0, len(rooms))
	for _, room := range rooms {
		owners = append(owners, room.Id, m.Id)
		members = append(members, room.Id)
	}
	pipe := m.client.Pipeline()
	pipe.HSet(ctx, roomOwnersKey, owners...)
	pipe.SAdd(ctx, globalRoomsKey, members...)
	_, err := pipe.Exec(ctx)
	_ = pipe.Close()
	return err
}

func (m *redisRoomManager) closeRemoteRooms(ctx context.Context) error {
	rooms := m.remoteRoomCache.CloseRooms()
	if len(rooms) == 0 {
		return nil
	}
	pipe := m.client.Pipeline()
	for _, room := range rooms {
		req := m.createRoomEventRequest(room.Id, LeaveEvent)
		body, _ := json.Marshal(&req)
		pipe.Publish(ctx, eventsChannel, body)
	}
	_, err := pipe.Exec(ctx)
	_ = pipe.Close()
	return err
}

func (m *redisRoomManager) removeRoomOwner(ctx context.Context, roomId string) (bool, error) {
	const script = `
if redis.call('hget', KEYS[1], ARGV[1]) == ARGV[2] then
    return redis.call('hdel', KEYS[1], ARGV[1])
end
return 0`
	removed, err := m.client.Eval(ctx, script, []string{roomOwnersKey}, roomId, m.Id).Int()
	return removed > 0, err
}

func (m *redisRoomManager) removeStaleRoom(ctx context.Context, roomId, owner string) error {
	alive, err := m.client.Exists(ctx, managerLeaseKey(owner)).Result()
	if err != nil {
		return err
	}
	if alive > 0 {
		return nil
	}
	if err := m.client.SRem(ctx, globalRoomsKey, roomId).Err(); err != nil {
		return err
	}
	const script = `
if redis.call('hget', KEYS[1], ARGV[1]) == ARGV[2] then
    return redis.call('hdel', KEYS[1], ARGV[1])
end
return 0`
	removed, err := m.client.Eval(ctx, script, []string{roomOwnersKey}, roomId, owner).Int()
	if err != nil {
		return err
	}
	if removed == 0 {
		currentOwner, getErr := m.client.HGet(ctx, roomOwnersKey, roomId).Result()
		if getErr != nil && !errors.Is(getErr, redis.Nil) {
			return getErr
		}
		if currentOwner != "" && currentOwner != owner {
			return m.client.SAdd(ctx, globalRoomsKey, roomId).Err()
		}
	}
	alive, err = m.client.Exists(ctx, managerLeaseKey(owner)).Result()
	if err != nil {
		return err
	}
	if alive > 0 {
		if err := m.client.HSet(ctx, roomOwnersKey, roomId, owner).Err(); err != nil {
			return err
		}
		return m.client.SAdd(ctx, globalRoomsKey, roomId).Err()
	}
	req := m.createRoomEventRequest(roomId, ExitEvent)
	if err := m.publishRequest(&req); err != nil {
		logger.Errorf("Redis cache publish stale room %s exit event err: %s", roomId, err)
	}
	return nil
}

func (m *redisRoomManager) publishCommand(channel string, p []byte) error {
	select {
	case <-m.done:
		return errRedisManagerClosed
	default:
	}
	ctx, cancel := context.WithTimeout(context.Background(), redisCommandTimeout)
	defer cancel()
	return m.client.Publish(ctx, channel, p).Err()
}

func (m *redisRoomManager) markDone() {
	m.doneOnce.Do(func() {
		close(m.done)
	})
}

func (m *redisRoomManager) Close() error {
	m.closeOnce.Do(func() {
		m.markDone()
		m.remoteRoomCache.Close()
		m.localRoomCache.Close()
		ctx, cancel := context.WithTimeout(context.Background(), redisCloseTimeout)
		leaseErr := m.client.Del(ctx, managerLeaseKey(m.Id)).Err()
		cancel()
		m.closeErr = errors.Join(leaseErr, m.pubSub.Close(), m.client.Close())
	})
	return m.closeErr
}

func (m *redisRoomManager) run() {
	requestsMap := make(map[string]*managerRequest)
	heartbeat := time.NewTicker(managerHeartbeat)
	defer heartbeat.Stop()
	needsRoomRestore := false

	// 本地 Room 增加 redisCon，key 是 room id
	redisUserCons := make(map[string]*redisUserConState)
	defer func() {
		m.markDone()
		for _, request := range requestsMap {
			request.response <- &subscribeResponse{
				Req: request.req,
				err: errRedisManagerClosed,
			}
		}
		for _, state := range redisUserCons {
			_ = state.channel.Close()
		}
	}()

	for {
		select {
		case <-m.done:
			return
		case <-heartbeat.C:
			ctx, cancel := context.WithTimeout(context.Background(), redisCommandTimeout)
			expired, err := m.renewLease(ctx)
			if expired {
				err = m.closeRemoteRooms(ctx)
			}
			needsRoomRestore = needsRoomRestore || expired
			if err == nil && needsRoomRestore {
				err = m.restoreLocalRooms(ctx)
				if err == nil {
					needsRoomRestore = false
				}
			}
			if err == nil {
				err = m.removeExpiredSubscribers(ctx, redisUserCons)
			}
			if err == nil {
				err = m.cleanupStaleRooms(ctx)
			}
			cancel()
			if err != nil {
				logger.Errorf("Redis cache refresh manager lease err: %s", err)
			}
			for reqId, request := range requestsMap {
				select {
				case <-request.done:
					delete(requestsMap, reqId)
				default:
				}
			}

		case request := <-m.reqChan:
			req := request.req
			switch req.Event {
			case JoinEvent:
				//	校验本地 是否已经存在
				if room := m.remoteRoomCache.Get(req.RoomId); room != nil {
					logger.Debugf("Redis cache already create room %s", req.RoomId)
					request.response <- &subscribeResponse{
						Req:  req,
						room: room,
						err:  nil,
					}
					continue
				}
				// 本地不存在则发送请求信号
				requestsMap[req.ReqId] = request
				if err := m.publishRequest(req); err != nil {
					logger.Debugf("Redis cache send request join room %s err: %s", req.RoomId, err)
					delete(requestsMap, req.ReqId)
					request.response <- &subscribeResponse{
						Req:  req,
						room: nil,
						err:  err,
					}
					continue
				}
			case ExitEvent:
				if err := m.publishRequest(req); err != nil {
					request.response <- &subscribeResponse{
						Req: req,
						err: err,
					}
					logger.Errorf("Redis cache send request %s event %s err: %s", req.ReqId, req.Event, err)
					continue
				}
				request.response <- &subscribeResponse{Req: req}
			default:
				request.response <- &subscribeResponse{
					Req: req,
					err: fmt.Errorf("unsupported Redis room event %s", req.Event),
				}
			}
			logger.Debugf("Redis cache send event %s for room %s", req.Event, req.RoomId)

		case reqId := <-m.reqCancelChan:
			delete(requestsMap, reqId)
			logger.Debugf("Redis cache cancel request %s", reqId)

		case redisUserCon := <-m.removeRedisUserConChan:
			state := redisUserCons[redisUserCon.roomId]
			if state != nil && state.channel == redisUserCon {
				delete(redisUserCons, redisUserCon.roomId)
				req := m.createRoomEventRequest(redisUserCon.roomId, ExitEvent)
				if err := m.publishRequest(&req); err != nil {
					logger.Errorf("Redis cache publish broken room %s proxy exit err: %s", redisUserCon.roomId, err)
				}
			}

		case room := <-m.removeProxyRoomChan:
			if !m.remoteRoomCache.DeleteIf(room) {
				continue
			}
			logger.Infof("Redis cache delete remote room %s", room.Id)
			req := m.createRoomEventRequest(room.Id, LeaveEvent)
			if err := m.publishRequest(&req); err != nil {
				logger.Errorf("Redis cache send leave event for room %s err: %s", room.Id, err)
			} else {
				logger.Debugf("Redis cache send leave event for room %s success", room.Id)
			}

		case redisMsg, ok := <-m.subscribeEventsMsgCh:
			if !ok {
				return
			}
			var req subscribeRequest
			if err := json.Unmarshal([]byte(redisMsg.Payload), &req); err != nil {
				logger.Errorf("Redis cache unmarshal request msg err: %s", err)
				continue
			}

			switch redisMsg.Channel {
			case resultsChannel:
				switch req.Event {
				case JoinSuccessEvent:
					request, ok := requestsMap[req.ReqId]
					if !ok {
						logger.Debugf("Redis cache ignore not self result request %s", req.ReqId)
						continue
					}
					logger.Infof("Redis cache request %s receive result", req.ReqId)
					// 请求结束，移除缓存, 返回请求的结果
					delete(requestsMap, req.ReqId)

					request.response <- &subscribeResponse{Req: &req}
					logger.Infof("Redis cache request %s finished", req.ReqId)
				default:
					logger.Infof("Result channel receive unhandled event %s", req.Event)
				}

			case eventsChannel:
				switch req.Event {
				case JoinEvent:
					/*
						1. 检查是否是自己创建的req: 是则忽略
						2. 检查是否已经创建过redisUserCon: 是则发送JoinSuccessEvent
						3. 检查是否是本KOKO创建的Session会话: 是则创建redisUserCon，并发送JoinSuccessEvent
					*/

					if req.ManagerId == m.Id {
						logger.Debugf("Redis cache ignore self request %s", req.ReqId)
						continue
					}
					if _, ok := requestsMap[req.ReqId]; ok {
						logger.Debugf("Redis cache ignore self request %s", req.ReqId)
						continue
					}
					// 创建result channel的req
					successReq := m.createRoomResultRequest(req.ReqId,
						req.RoomId, JoinSuccessEvent)

					// 本地是否已经创建过 redisUserCons
					if state, ok := redisUserCons[req.RoomId]; ok {
						logger.Infof("Redis cache already create redis con for room %s", req.RoomId)
						added := state.add(&req)
						if err := m.publishRequest(&successReq); err != nil {
							logger.Errorf("Redis cache reply request %s join event err %s", req.ReqId, err)
							if added {
								state.remove(&req)
								if state.count() == 0 {
									closeRedisUserCon(redisUserCons, req.RoomId, state)
								}
							}
						} else {
							logger.Infof("Redis cache reply request %s join event", req.ReqId)
						}
						continue
					}

					// 如果是当前节点 KoKo 创建的session
					if r := m.localRoomCache.Get(req.RoomId); r != nil {
						writeChannel := createSessionChannel(fmt.Sprintf("%s.read", req.RoomId))
						readChannel := createSessionChannel(fmt.Sprintf("%s.write", req.RoomId))
						pubSub, subMsgCh, err := subscribeRedisChannels(
							m.client, m.subscribeTimeout, readChannel,
						)
						if err != nil {
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
						}

						state := &redisUserConState{
							channel:     s,
							subscribers: make(map[string]struct{}),
						}
						state.add(&req)
						redisUserCons[req.RoomId] = state
						go proxyUserCon(r, s)
						if err := m.publishRequest(&successReq); err != nil {
							logger.Errorf("Redis cache reply request %s join event err %s", req.ReqId, err)
							state.remove(&req)
							if state.count() == 0 {
								closeRedisUserCon(redisUserCons, req.RoomId, state)
							}
						} else {
							logger.Infof("Redis cache reply request %s join event", req.ReqId)
						}
						continue
					}
					logger.Infof("The current KoKo node has no session room %s", req.RoomId)
					// 非本节点 koko 创建的session
				case LeaveEvent:
					if state, ok := redisUserCons[req.RoomId]; ok {
						if state.remove(&req) {
							if state.count() == 0 {
								closeRedisUserCon(redisUserCons, req.RoomId, state)
							}
						}
						logger.Infof("Event channel receive room %s leave event", req.RoomId)
					}

				case ExitEvent:
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
	if room := m.remoteRoomCache.Get(roomId); room != nil {
		return room
	}

	m.joinMu.Lock()
	if call := m.joining[roomId]; call != nil {
		m.joinMu.Unlock()
		select {
		case <-call.done:
			return call.room
		case <-m.done:
			return nil
		}
	}
	call := &joinCall{done: make(chan struct{})}
	m.joining[roomId] = call
	m.joinMu.Unlock()

	room, err := m.joinRemoteSessionRoom(roomId)
	m.joinMu.Lock()
	call.room = room
	delete(m.joining, roomId)
	close(call.done)
	m.joinMu.Unlock()
	if err != nil {
		logger.Errorf("get remote session room err: %s", err)
	}
	return room
}

func (m *redisRoomManager) joinRemoteSessionRoom(roomId string) (*Room, error) {
	readChannel := createSessionChannel(fmt.Sprintf("%s.read", roomId))
	pubSub, redisMsgCh, err := subscribeRedisChannels(
		m.client, m.subscribeTimeout, readChannel,
	)
	if err != nil {
		return nil, err
	}
	handedOff := false
	defer func() {
		if !handedOff {
			_ = pubSub.Close()
		}
	}()

	req := m.createRoomEventRequest(roomId, JoinEvent)
	res, err := m.sendJoinRequest(&req)
	if err != nil {
		leaveReq := m.createRoomEventRequest(roomId, LeaveEvent)
		if publishErr := m.publishRequest(&leaveReq); publishErr != nil {
			logger.Errorf("Redis cache rollback room %s join err: %s", roomId, publishErr)
		}
		return nil, err
	}
	if res.room != nil {
		return res.room, nil
	}

	userInputChan := make(chan *RoomMessage)
	room := CreateRoom(roomId, userInputChan)
	room.forwardEvents = userInputChan
	cacheRoom, added := m.remoteRoomCache.AddIfAbsent(room)
	if !added {
		return cacheRoom, nil
	}
	s := &redisChannel{
		roomId:       roomId,
		writeChannel: createSessionChannel(fmt.Sprintf("%s.write", roomId)),
		readChannel:  readChannel,
		pubSub:       pubSub,
		subMsgCh:     redisMsgCh,
		manager:      m,
		done:         make(chan struct{}),
	}
	handedOff = true
	go proxyRoom(room, s, userInputChan)
	return room, nil
}

func (m *redisRoomManager) uniqueReqId(sid string) string {
	return fmt.Sprintf("%s:%s:%s", m.Id, sid, common.UUID())
}

func (m *redisRoomManager) sendJoinRequest(req *subscribeRequest) (*subscribeResponse, error) {
	return m.sendRequest(req)
}

func (m *redisRoomManager) sendRequest(req *subscribeRequest) (*subscribeResponse, error) {
	ctx, cancelFunc := context.WithTimeout(context.Background(), redisRequestTimeout)
	defer cancelFunc()
	request := &managerRequest{
		req:      req,
		response: make(chan *subscribeResponse, 1),
		done:     ctx.Done(),
	}
	select {
	case m.reqChan <- request:
	case <-m.done:
		return nil, errRedisManagerClosed
	case <-ctx.Done():
		return nil, fmt.Errorf("Redis cache send request event %s time out: %w", req.Event, ctx.Err())
	}

	select {
	case res := <-request.response:
		return res, res.err
	case <-m.done:
		return nil, errRedisManagerClosed
	case <-ctx.Done():
		select {
		case m.reqCancelChan <- req.ReqId:
		case <-m.done:
		default:
		}
		return nil, fmt.Errorf("Redis cache send request event %s time out: %w", req.Event, ctx.Err())
	}
}

func (m *redisRoomManager) publishRequest(req *subscribeRequest) error {
	body, _ := json.Marshal(req)
	return m.publishCommand(req.Channel, body)
}

func (m *redisRoomManager) createRoomEventRequest(roomId, event string) subscribeRequest {
	return subscribeRequest{
		ReqId:     m.uniqueReqId(roomId),
		RoomId:    roomId,
		Event:     event,
		Channel:   eventsChannel,
		ManagerId: m.Id,
	}
}

func (m *redisRoomManager) createRoomResultRequest(reqId, roomId, event string) subscribeRequest {
	return subscribeRequest{
		ReqId:     reqId,
		RoomId:    roomId,
		Event:     event,
		Channel:   resultsChannel,
		ManagerId: m.Id,
	}
}

type subscribeResponse struct {
	Req  *subscribeRequest
	room *Room
	err  error
}

type subscribeRequest struct {
	ReqId     string `json:"req_id"` //
	RoomId    string `json:"room_id"`
	Event     string `json:"event"`
	ManagerId string `json:"manager_id,omitempty"`
	Channel   string `json:"-"`
}

func createSessionChannel(channel string) string {
	return fmt.Sprintf("%s%s", sessionsChannelPrefix, channel)
}
