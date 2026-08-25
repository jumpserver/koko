package tunnel

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/go-redis/redis/v8"

	"github.com/jumpserver/koko/pkg/lion/guacd"
	"github.com/jumpserver/koko/pkg/logger"

	"github.com/jumpserver-dev/sdk-go/common"
)

const (
	eventsChannel = "JUMPSERVER:LION:EVENTS:CHANNEL"

	resultsChannel = "JUMPSERVER:LION:EVENTS:RESULT"

	sessionEventsChannel = "JUMPSERVER:LION:EVENTS:SESSIONS"

	sessionsChannelPrefix = "JUMPSERVER:LION:SESSIONS"

	redisOperationTimeout = 5 * time.Second
	redisRequestTimeout   = 20 * time.Second
)

type Config struct {
	// Addr of a single redis server instance.
	// Defaults to "127.0.0.1:6379".
	Addr string

	Password string

	DBIndex int

	SentinelPassword string
	SentinelsHost    string
	UseSSL           bool
	SSLCa            string
	SSLCert          string
	SSLKey           string
}

func getRedisTLSCfg(conf *Config) (*tls.Config, error) {
	tlsCfg := tls.Config{}
	if conf.SSLCert != "" && conf.SSLKey != "" {
		cert, err := tls.LoadX509KeyPair(conf.SSLCert, conf.SSLKey)
		if err != nil {
			return nil, err
		}
		logger.Debugf("Load redis SSL cert: %s, key: %s", conf.SSLCert, conf.SSLKey)
		tlsCfg.Certificates = []tls.Certificate{cert}
		tlsCfg.InsecureSkipVerify = true
	}
	if conf.SSLCa != "" {
		certPool := x509.NewCertPool()
		buf, err := os.ReadFile(conf.SSLCa)
		if err != nil {
			return nil, err
		}
		logger.Debugf("Load redis SSL ca: %s", conf.SSLCa)
		if !certPool.AppendCertsFromPEM(buf) {
			return nil, errors.New("redis CA file does not contain a valid certificate")
		}
		tlsCfg.RootCAs = certPool
		tlsCfg.InsecureSkipVerify = true
	}
	return &tlsCfg, nil
}

func NewGuaTunnelRedisCache(conf Config) (*GuaTunnelRedisCache, error) {
	if conf.Addr == "" {
		conf.Addr = "127.0.0.1:6379"
	}
	var (
		rdb    *redis.Client
		tlsCfg *tls.Config
		err    error
		dialer func(ctx context.Context, network, addr string) (net.Conn, error)
	)
	if conf.UseSSL {
		tlsCfg, err = getRedisTLSCfg(&conf)
		if err != nil {
			return nil, fmt.Errorf("redis TLS config: %w", err)
		}
		tlsDialer := tls.Dialer{Config: tlsCfg}
		dialer = func(ctx context.Context, network, addr string) (net.Conn, error) {
			return tlsDialer.DialContext(ctx, network, addr)
		}
	}
	if conf.SentinelsHost != "" {
		sentinels := strings.SplitN(conf.SentinelsHost, "/", 2)
		if len(sentinels) != 2 || strings.TrimSpace(sentinels[0]) == "" || strings.TrimSpace(sentinels[1]) == "" {
			return nil, fmt.Errorf("invalid sentinel host: %s", conf.SentinelsHost)
		}
		sentinelServiceName := strings.TrimSpace(sentinels[0])
		sentinelHosts := make([]string, 0)
		for _, host := range strings.Split(sentinels[1], ",") {
			if host = strings.TrimSpace(host); host != "" {
				sentinelHosts = append(sentinelHosts, host)
			}
		}
		if len(sentinelHosts) == 0 {
			return nil, fmt.Errorf("invalid sentinel host: %s", conf.SentinelsHost)
		}

		rdb = redis.NewFailoverClient(&redis.FailoverOptions{
			MasterName:       sentinelServiceName,
			SentinelAddrs:    sentinelHosts,
			SentinelPassword: conf.SentinelPassword,
			Password:         conf.Password,
			DB:               conf.DBIndex,
			TLSConfig:        tlsCfg,
			Dialer:           dialer,
		})
	} else {
		rdb = redis.NewClient(&redis.Options{
			Addr:      conf.Addr,
			Password:  conf.Password,
			DB:        conf.DBIndex,
			TLSConfig: tlsCfg,
		})
	}

	pingCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := rdb.Ping(pingCtx).Result(); err != nil {
		_ = rdb.Close()
		return nil, fmt.Errorf("redis ping: %w", err)
	}

	cache := GuaTunnelRedisCache{
		ID:                  common.UUID(),
		rdb:                 rdb,
		requests:            make(map[string]chan *subscribeResponse),
		redisConAddChan:     make(chan *RedisConn),
		redisProxyExitChan:  make(chan string, 100),
		redisConExitChan:    make(chan string, 100),
		done:                make(chan struct{}),
		runDone:             make(chan struct{}),
		GuaTunnelLocalCache: NewLocalTunnelLocalCache(),
	}
	subscribeCtx, subscribeCancel := context.WithTimeout(context.Background(), redisOperationTimeout)
	defer subscribeCancel()
	innerPubSub, err := cache.subscribe(subscribeCtx, eventsChannel, resultsChannel)
	if err != nil {
		_ = rdb.Close()
		return nil, fmt.Errorf("subscribe to Redis control channels: %w", err)
	}
	sessionPubSub, err := cache.subscribe(subscribeCtx, sessionEventsChannel)
	if err != nil {
		_ = innerPubSub.Close()
		_ = rdb.Close()
		return nil, fmt.Errorf("subscribe to Redis session channel: %w", err)
	}
	go cache.run(innerPubSub, sessionPubSub)
	return &cache, nil
}

type GuaTunnelRedisCache struct {
	*GuaTunnelLocalCache

	ID  string
	rdb *redis.Client

	requestsMu sync.Mutex
	requests   map[string]chan *subscribeResponse

	redisConAddChan    chan *RedisConn
	redisProxyExitChan chan string
	redisConExitChan   chan string

	done      chan struct{}
	runDone   chan struct{}
	closeOnce sync.Once
}

func (r *GuaTunnelRedisCache) Close() error {
	var err error
	r.closeOnce.Do(func() {
		close(r.done)
		select {
		case <-r.runDone:
		case <-time.After(2 * time.Second):
			logger.Warn("Wait for Lion Redis room cache shutdown timed out")
		}
		err = r.rdb.Close()
	})
	return err
}

func (r *GuaTunnelRedisCache) BroadcastSessionEvent(sid string, event *Event) {
	r.GuaTunnelLocalCache.BroadcastSessionEvent(sid, event)
	r.broadcastSessionEventToRemote(sid, event)
}

func (r *GuaTunnelRedisCache) broadcastSessionEventToRemote(sid string, event *Event) {
	msg := SessionRoomMessage{
		Id:        r.ID,
		SessionId: sid,
		Event:     event,
	}
	eventBody, _ := json.Marshal(msg)
	if err := r.publishCommand(sessionEventsChannel, eventBody); err != nil {
		logger.Errorf("Redis cache broadcast session event %s err: %s", sid, err)
	}
}

func (r *GuaTunnelRedisCache) GetMonitorTunnelerBySessionId(sid string) Tunneler {
	tunneler := r.GuaTunnelLocalCache.GetMonitorTunnelerBySessionId(sid)
	if tunneler != nil {
		return tunneler
	}
	return r.requestRemoteTunnelerBySessionId(sid)
}

func (r *GuaTunnelRedisCache) requestRemoteTunnelerBySessionId(sid string) Tunneler {
	req := r.createEventRequest(sid, channelEventJoin)
	ctx, cancel := context.WithTimeout(context.Background(), redisRequestTimeout)
	defer cancel()
	readChannel := fmt.Sprintf("%s.read", req.Prefix)
	pubSub, err := r.subscribe(ctx, readChannel)
	if err != nil {
		logger.Errorf("Redis cache subscribe session %s read channel: %s", sid, err)
		return nil
	}
	conn := &RedisConn{
		reqId:            req.ReqId,
		sessionId:        req.SessionId,
		readChannelName:  readChannel,
		writeChannelName: fmt.Sprintf("%s.write", req.Prefix),
		instructionChan:  make(chan guacd.Instruction, 100),
		cache:            r,
		pubSub:           pubSub,
		done:             make(chan struct{}),
	}
	select {
	case <-r.done:
		_ = conn.Close()
		return nil
	case <-r.runDone:
		_ = conn.Close()
		return nil
	case <-ctx.Done():
		_ = conn.Close()
		return nil
	case r.redisConAddChan <- conn:
	}
	go conn.run()
	res, err := r.sendRequest(ctx, &req)
	if err != nil {
		_ = conn.Close()
		logger.Error(err)
		return nil
	}
	conn.uuid = res.Req.UUID
	return conn
}

func (r *GuaTunnelRedisCache) sendRequest(ctx context.Context, req *subscribeRequest) (*subscribeResponse, error) {
	resultChan := make(chan *subscribeResponse, 1)
	r.requestsMu.Lock()
	if _, ok := r.requests[req.ReqId]; ok {
		r.requestsMu.Unlock()
		return nil, fmt.Errorf("Redis cache request %s already exists", req.ReqId)
	}
	r.requests[req.ReqId] = resultChan
	r.requestsMu.Unlock()
	defer r.deleteRequest(req.ReqId, resultChan)

	if err := r.publishRequest(ctx, req); err != nil {
		return nil, err
	}
	logger.Infof("Redis cache publish request %s event %s success", req.ReqId, req.Event)
	select {
	case <-r.done:
		return nil, errors.New("Redis cache is closed")
	case <-r.runDone:
		return nil, errors.New("Redis cache subscriber is closed")
	case <-ctx.Done():
		return nil, fmt.Errorf("Redis cache send request event %s: %w", req.Event, ctx.Err())
	case res := <-resultChan:
		return res, res.err
	}
}

func (r *GuaTunnelRedisCache) getRequest(reqId string) (chan *subscribeResponse, bool) {
	r.requestsMu.Lock()
	defer r.requestsMu.Unlock()
	responseChan, ok := r.requests[reqId]
	return responseChan, ok
}

func (r *GuaTunnelRedisCache) deleteRequest(reqId string, responseChan chan *subscribeResponse) {
	r.requestsMu.Lock()
	defer r.requestsMu.Unlock()
	if current, ok := r.requests[reqId]; ok && current == responseChan {
		delete(r.requests, reqId)
	}
}

func (r *GuaTunnelRedisCache) createEventRequest(sid, event string) subscribeRequest {
	reqId := r.uniqueReqId(sid)
	return subscribeRequest{
		ReqId:     reqId,
		SessionId: sid,
		Event:     event,
		Prefix:    reqId,
		Channel:   eventsChannel,
	}
}

func (r *GuaTunnelRedisCache) createResultRequest(reqId, roomId, event string) subscribeRequest {
	return subscribeRequest{
		ReqId:     reqId,
		SessionId: roomId,
		Event:     event,
		Prefix:    reqId,
		Channel:   resultsChannel,
	}
}

/*
(确保每次都是唯一的)
prefix: sessionsChannelPrefix:uuid:reqId:sessionId

*/

func (r *GuaTunnelRedisCache) uniqueReqId(sid string) string {
	return fmt.Sprintf("%s:%s:%s:%s",
		sessionsChannelPrefix,
		common.UUID(),
		r.ID,
		sid)
}

func (r *GuaTunnelRedisCache) publishRequest(ctx context.Context, req *subscribeRequest) error {
	body, _ := json.Marshal(req)
	return r.publishCommandContext(ctx, req.Channel, body)
}

func (r *GuaTunnelRedisCache) publishCommand(channel string, p []byte) error {
	ctx, cancel := context.WithTimeout(context.Background(), redisOperationTimeout)
	defer cancel()
	return r.publishCommandContext(ctx, channel, p)
}

func (r *GuaTunnelRedisCache) publishCommandContext(ctx context.Context, channel string, p []byte) error {
	return r.rdb.Publish(ctx, channel, p).Err()
}

func (r *GuaTunnelRedisCache) subscribe(ctx context.Context, channels ...string) (*redis.PubSub, error) {
	pubSub := r.rdb.Subscribe(ctx, channels...)
	pending := make(map[string]struct{}, len(channels))
	for _, channel := range channels {
		pending[channel] = struct{}{}
	}
	for len(pending) > 0 {
		message, err := pubSub.Receive(ctx)
		if err != nil {
			_ = pubSub.Close()
			return nil, err
		}
		subscription, ok := message.(*redis.Subscription)
		if !ok || subscription.Kind != "subscribe" {
			_ = pubSub.Close()
			return nil, fmt.Errorf("unexpected Redis subscription response %T", message)
		}
		delete(pending, subscription.Channel)
	}
	pubSub.Channel()
	return pubSub, nil
}

func (r *GuaTunnelRedisCache) proxyTunnel(tunnelProxy *RedisGuacProxy) {
	defer func() {
		r.GuaTunnelLocalCache.RemoveMonitorTunneler(tunnelProxy.sessionId, tunnelProxy.tunnel)
		select {
		case r.redisProxyExitChan <- tunnelProxy.reqId:
		case <-r.done:
		case <-r.runDone:
		default:
		}
		if !tunnelProxy.remoteClosed.Load() {
			r.publishExit(tunnelProxy.reqId, tunnelProxy.sessionId)
		}
		_ = tunnelProxy.Close()
	}()
	logger.Infof("Redis guacd proxy %s tunnel start", tunnelProxy.reqId)
	for {
		ins, err := tunnelProxy.ReadInstruction()
		if err != nil {
			logger.Errorf("Redis guacd proxy %s tunnel read err: %s", tunnelProxy.reqId, err)
			return
		}
		if err = r.publishCommand(tunnelProxy.writeChannelName, []byte(ins.String())); err != nil {
			logger.Errorf("Redis guacd proxy %s pubSub message err: %s", tunnelProxy.reqId, err)
		}
	}
}

func (r *GuaTunnelRedisCache) publishExit(reqId, sessionId string) {
	select {
	case <-r.done:
		return
	case <-r.runDone:
		return
	default:
	}
	ctx, cancel := context.WithTimeout(context.Background(), redisOperationTimeout)
	defer cancel()
	if err := r.publishRequest(ctx, &subscribeRequest{
		ReqId:     reqId,
		SessionId: sessionId,
		Event:     channelEventExit,
		Prefix:    reqId,
		Channel:   eventsChannel,
	}); err != nil {
		logger.Errorf("Redis cache publish %s exit event: %s", reqId, err)
	}
}

func (r *GuaTunnelRedisCache) run(innerPubSub, sessionPubSub *redis.PubSub) {
	proxyConnMap := make(map[string]*RedisGuacProxy)
	localConnMap := make(map[string]*RedisConn)
	defer func() {
		for _, connection := range proxyConnMap {
			_ = connection.Close()
		}
		for _, connection := range localConnMap {
			_ = connection.Close()
		}
		_ = innerPubSub.Close()
		_ = sessionPubSub.Close()
		close(r.runDone)
	}()
	subscribeEventsMsgCh := innerPubSub.Channel()
	sessionEventsMsgCh := sessionPubSub.Channel()
	for {
		select {
		case <-r.done:
			return
		case redisMsg, ok := <-subscribeEventsMsgCh:
			if !ok {
				return
			}
			var req subscribeRequest
			if err := json.Unmarshal([]byte(redisMsg.Payload), &req); err != nil {
				logger.Errorf("Redis cache unmarshal request msg err: %s", err)
				continue
			}
			logger.Infof("Redis channel %s recv request event %s",
				redisMsg.Channel, req.Event)

			switch redisMsg.Channel {
			case eventsChannel:
				if _, ok := r.getRequest(req.ReqId); ok {
					logger.Infof("Redis cache ignore self request %s", req.ReqId)
					continue
				}
				// 创建result channel的req
				switch req.Event {
				case channelEventJoin:
					successReq := r.createResultRequest(req.ReqId, req.SessionId,
						channelEventJoinSuccess)
					if conn := r.GuaTunnelLocalCache.GetBySessionId(req.SessionId); conn != nil {
						guacdTunnel, err := conn.CloneMonitorTunnel()
						if err != nil {
							logger.Errorf("Redis cache create monitor tunneler for request %s: %s",
								req.ReqId, err)
							continue
						}
						successReq.UUID = guacdTunnel.UUID()
						writeChannel := fmt.Sprintf("%s.read", req.Prefix)
						readChannel := fmt.Sprintf("%s.write", req.Prefix)
						subscribeCtx, cancel := context.WithTimeout(context.Background(), redisOperationTimeout)
						pubSub, subscribeErr := r.subscribe(subscribeCtx, readChannel)
						cancel()
						if subscribeErr != nil {
							_ = guacdTunnel.Close()
							r.GuaTunnelLocalCache.RemoveMonitorTunneler(req.SessionId, guacdTunnel)
							logger.Errorf("Redis cache subscribe request %s write channel: %s",
								req.ReqId, subscribeErr)
							continue
						}
						proxyConn := RedisGuacProxy{
							reqId:            req.ReqId,
							sessionId:        req.SessionId,
							readChannelName:  readChannel,
							writeChannelName: writeChannel,
							pubSub:           pubSub,
							cache:            r,
							done:             make(chan struct{}),
							tunnel:           guacdTunnel,
						}
						publishCtx, publishCancel := context.WithTimeout(context.Background(), redisOperationTimeout)
						err = r.publishRequest(publishCtx, &successReq)
						publishCancel()
						if err != nil {
							_ = proxyConn.Close()
							r.GuaTunnelLocalCache.RemoveMonitorTunneler(req.SessionId, guacdTunnel)
							logger.Errorf("Redis cache reply request %s join event err %s", req.ReqId, err)
							continue
						}
						logger.Infof("Redis cache reply request %s join event", req.ReqId)
						proxyConnMap[req.ReqId] = &proxyConn
						go proxyConn.run()
						go r.proxyTunnel(&proxyConn)
					}

				case channelEventExit:
					successReq := r.createResultRequest(req.ReqId, req.SessionId,
						channelEventExitSuccess)
					matched := false
					if proxyConn, ok := proxyConnMap[req.ReqId]; ok {
						logger.Infof("Redis cache reply %s exit event", req.ReqId)
						matched = true
						proxyConn.remoteClosed.Store(true)
						_ = proxyConn.Close()
					}
					if redisConn, ok := localConnMap[req.ReqId]; ok {
						matched = true
						redisConn.remoteClosed.Store(true)
						_ = redisConn.Close()
					}
					if matched {
						publishCtx, publishCancel := context.WithTimeout(context.Background(), redisOperationTimeout)
						err := r.publishRequest(publishCtx, &successReq)
						publishCancel()
						if err != nil {
							logger.Errorf("Redis cache reply request %s exit event err %s", req.ReqId, err)
						}
					}
				}

			case resultsChannel:
				responseChan, ok := r.getRequest(req.ReqId)
				if !ok {
					logger.Debugf("Redis cache ignore not self result request %s", req.ReqId)
					continue
				}
				logger.Infof("Redis cache request %s receive result event %s", req.ReqId, req.Event)
				switch req.Event {
				case channelEventJoinSuccess:
					select {
					case responseChan <- &subscribeResponse{Req: &req}:
					default:
					}
				case channelEventExitSuccess:
					select {
					case responseChan <- &subscribeResponse{Req: &req}:
					default:
					}
				}
			default:
				continue
			}
		case redisSessionMsg, ok := <-sessionEventsMsgCh:
			if !ok {
				return
			}
			var msg SessionRoomMessage
			if err := json.Unmarshal([]byte(redisSessionMsg.Payload), &msg); err != nil {
				logger.Errorf("Redis cache unmarshal session event msg err: %s", err)
				continue
			}
			if msg.Event == nil {
				logger.Errorf("Redis cache session event %s has no payload", msg.SessionId)
				continue
			}
			if msg.Id == r.ID {
				logger.Debugf("Redis cache ignore self session event %s", msg.Event.Type)
				continue
			}
			logger.Infof("Redis channel %s recv session event %s",
				redisSessionMsg.Channel, msg.Event.Type)
			r.GuaTunnelLocalCache.BroadcastSessionEvent(msg.SessionId, msg.Event)

		case conn := <-r.redisConAddChan:
			localConnMap[conn.reqId] = conn
		case reqId := <-r.redisProxyExitChan:
			if _, ok := proxyConnMap[reqId]; ok {
				logger.Infof("Redis cache recv proxy conn %s exit signal", reqId)
				delete(proxyConnMap, reqId)
			}
		case reqId := <-r.redisConExitChan:
			if _, ok := localConnMap[reqId]; ok {
				logger.Infof("Redis cache recv redis conn %s exit signal", reqId)
				delete(localConnMap, reqId)
			}
		}
	}

}

type RedisConn struct {
	reqId     string
	sessionId string
	uuid      string

	readChannelName  string
	writeChannelName string
	instructionChan  chan guacd.Instruction
	cache            *GuaTunnelRedisCache
	once             sync.Once
	remoteClosed     atomic.Bool
	pubSub           *redis.PubSub

	done chan struct{}
}

func (r *RedisConn) UUID() string {
	return r.uuid
}

func (r *RedisConn) run() {
	logger.Infof("Redis Conn %s pubSub run", r.reqId)
	messageChan := r.pubSub.Channel()
	defer close(r.instructionChan)
	detectTicker := time.NewTicker(time.Minute)
	defer detectTicker.Stop()
	activeTime := time.Now()
	defer func() {
		select {
		case r.cache.redisConExitChan <- r.reqId:
		case <-r.cache.done:
		case <-r.cache.runDone:
		default:
		}
		if !r.remoteClosed.Load() {
			r.cache.publishExit(r.reqId, r.sessionId)
		}
	}()
	for {
		select {
		case detectTime := <-detectTicker.C:
			if detectTime.After(activeTime.Add(5 * time.Minute)) {
				logger.Errorf("Redis Conn %s time out after 5 minute and exit.", r.reqId)
				return
			}
			continue
		case <-r.done:
			return
		case msg, ok := <-messageChan:
			if !ok {
				logger.Infof("Redis Conn %s pubSub exit", r.reqId)
				return
			}
			switch msg.Channel {
			case r.readChannelName:
				if ret, err := guacd.ParseInstructionString(msg.Payload); err == nil {
					select {
					case <-r.done:
						return
					case r.instructionChan <- ret:
					}
				} else {
					logger.Errorf("Redis Conn %s parse instruction err: %+v", r.reqId, err)
				}
			}
		}
		activeTime = time.Now()
	}
}

func (r *RedisConn) WriteAndFlush(p []byte) (int, error) {
	if err := r.cache.publishCommand(r.writeChannelName, p); err != nil {
		return 0, err
	}
	return len(p), nil
}

func (r *RedisConn) ReadInstruction() (guacd.Instruction, error) {
	if instruction, ok := <-r.instructionChan; ok {
		return instruction, nil
	}
	return guacd.Instruction{}, io.EOF
}

func (r *RedisConn) Close() error {
	var err error
	r.once.Do(func() {
		logger.Infof("Redis conn %s close", r.reqId)
		close(r.done)
		err = r.pubSub.Close()
	})
	return err
}

const (
	channelEventJoin        = "Join"
	channelEventExit        = "Exit"
	channelEventJoinSuccess = "JoinSuccess"
	channelEventExitSuccess = "ExitSuccess"
)

type subscribeRequest struct {
	ReqId     string `json:"req_id"`
	SessionId string `json:"session_id"`
	Event     string `json:"event"`
	Prefix    string `json:"prefix"`
	UUID      string `json:"uuid"`
	Channel   string `json:"-"`
}

type subscribeResponse struct {
	Req *subscribeRequest
	err error
}

type RedisGuacProxy struct {
	reqId     string
	sessionId string

	readChannelName  string
	writeChannelName string
	pubSub           *redis.PubSub

	cache *GuaTunnelRedisCache

	done         chan struct{}
	remoteClosed atomic.Bool

	tunnel *guacd.Tunnel

	once sync.Once
}

func (r *RedisGuacProxy) UUID() string {
	return r.tunnel.UUID()
}

func (r *RedisGuacProxy) run() {
	logger.Infof("Redis guacd proxy %s pubSub run", r.reqId)
	defer r.Close()
	redisMsgChan := r.pubSub.Channel()
	for {
		select {
		case redisMsg, ok := <-redisMsgChan:
			if !ok {
				logger.Infof("Redis guacd proxy %s pubSub exit", r.reqId)
				return
			}
			if _, err := r.tunnel.WriteAndFlush([]byte(redisMsg.Payload)); err != nil {
				logger.Errorf("Redis guacd proxy %s tunnel write err: %s", r.reqId, err)
				return
			}
		case <-r.done:
			return
		}
	}
}

func (r *RedisGuacProxy) ReadInstruction() (guacd.Instruction, error) {
	return r.tunnel.ReadInstruction()
}

func (r *RedisGuacProxy) Close() error {
	var err error
	r.once.Do(func() {
		err = r.pubSub.Close()
		_ = r.tunnel.Close()
		close(r.done)
		logger.Infof("Redis guacd proxy %s close", r.reqId)
	})
	return err
}
