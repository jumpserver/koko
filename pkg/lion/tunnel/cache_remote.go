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
		requestChan:         make(chan *subscribeRequest),
		responseChan:        make(chan chan *subscribeResponse),
		reqCancelChan:       make(chan *subscribeRequest),
		redisProxyExitChan:  make(chan string, 100),
		redisConExitChan:    make(chan string, 100),
		done:                make(chan struct{}),
		runDone:             make(chan struct{}),
		GuaTunnelLocalCache: NewLocalTunnelLocalCache(),
	}
	go cache.run()
	return &cache, nil
}

type GuaTunnelRedisCache struct {
	*GuaTunnelLocalCache

	ID  string
	rdb *redis.Client

	requestChan   chan *subscribeRequest
	responseChan  chan chan *subscribeResponse
	reqCancelChan chan *subscribeRequest

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
	/*
		1. 发布请求
		2. 收到Tunneler结果
	*/
	req := r.createEventRequest(sid, channelEventJoin)
	res, err := r.sendRequest(&req)
	if err != nil {
		logger.Error(err)
		return nil
	}
	return res.conn
}

func (r *GuaTunnelRedisCache) sendRequest(req *subscribeRequest) (*subscribeResponse, error) {
	ctx, cancelFunc := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancelFunc()
	select {
	case <-r.done:
		return nil, errors.New("Redis cache is closed")
	case r.requestChan <- req:
	}
	var resultChan chan *subscribeResponse
	select {
	case <-r.done:
		return nil, errors.New("Redis cache is closed")
	case resultChan = <-r.responseChan:
	}
	select {
	case <-r.done:
		return nil, errors.New("Redis cache is closed")
	case <-ctx.Done():
		select {
		case r.reqCancelChan <- req:

		case res := <-resultChan:
			return res, res.err
		}
	case res := <-resultChan:
		return res, res.err
	}
	return nil, fmt.Errorf("Redis cache send request event %s time out ", req.Event)
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

func (r *GuaTunnelRedisCache) publishRequest(req *subscribeRequest) error {
	body, _ := json.Marshal(req)
	return r.publishCommand(req.Channel, body)
}

func (r *GuaTunnelRedisCache) publishCommand(channel string, p []byte) (err error) {
	_, err = r.rdb.Publish(context.TODO(), channel, p).Result()
	return
}

func (r *GuaTunnelRedisCache) proxyTunnel(tunnelProxy *RedisGuacProxy) {
	defer func() {
		r.GuaTunnelLocalCache.RemoveMonitorTunneler(tunnelProxy.sessionId, tunnelProxy.tunnel)
		r.redisProxyExitChan <- tunnelProxy.reqId
		if _, err := r.sendRequest(&subscribeRequest{
			ReqId:     tunnelProxy.reqId,
			SessionId: tunnelProxy.sessionId,
			Event:     channelEventExit,
			Prefix:    tunnelProxy.reqId,
			Channel:   eventsChannel,
		}); err != nil {
			logger.Errorf("Redis guacd proxy pubSub exit event err: %v", err)
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

func (r *GuaTunnelRedisCache) run() {
	defer close(r.runDone)
	innerPubSub := r.rdb.Subscribe(context.TODO(), eventsChannel, resultsChannel)
	defer innerPubSub.Close()
	subscribeEventsMsgCh := innerPubSub.Channel()
	sessionPubSub := r.rdb.Subscribe(context.TODO(), sessionEventsChannel)
	defer sessionPubSub.Close()
	sessionEventsMsgCh := sessionPubSub.Channel()
	requestsMap := make(map[string]chan *subscribeResponse)
	proxyConnMap := make(map[string]*RedisGuacProxy)
	localConnMap := make(map[string]*RedisConn)
	for {
		select {
		case <-r.done:
			for _, connection := range proxyConnMap {
				_ = connection.Close()
			}
			for _, connection := range localConnMap {
				_ = connection.Close()
			}
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
				if _, ok := requestsMap[req.ReqId]; ok {
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
						err = r.publishRequest(&successReq)
						if err != nil {
							_ = guacdTunnel.Close()
							logger.Errorf("Redis cache reply request %s join event err %s", req.ReqId, err)
							continue
						}
						logger.Infof("Redis cache reply request %s join event", req.ReqId)
						writeChannel := fmt.Sprintf("%s.read", req.Prefix)
						readChannel := fmt.Sprintf("%s.write", req.Prefix)
						pubSub := r.rdb.Subscribe(context.TODO(), readChannel)
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
						proxyConnMap[req.ReqId] = &proxyConn
						go proxyConn.run()
						go r.proxyTunnel(&proxyConn)
					}

				case channelEventExit:
					successReq := r.createResultRequest(req.ReqId, req.SessionId,
						channelEventExitSuccess)
					if proxyConn, ok := proxyConnMap[req.ReqId]; ok {
						logger.Infof("Redis cache reply %s exit event", req.ReqId)
						err := r.publishRequest(&successReq)
						if err != nil {
							logger.Errorf("Redis cache reply request %s exit event err %s", req.ReqId, err)
						}
						_ = proxyConn.Close()
					}
					if redisConn, ok := localConnMap[req.ReqId]; ok {
						err := r.publishRequest(&successReq)
						if err != nil {
							logger.Errorf("Redis cache reply request %s exit event err %s", req.ReqId, err)
						}
						_ = redisConn.Close()
					}
				}

			case resultsChannel:
				responseChan, ok := requestsMap[req.ReqId]
				if !ok {
					logger.Debugf("Redis cache ignore not self result request %s", req.ReqId)
					continue
				}
				logger.Infof("Redis cache request %s receive result event %s", req.ReqId, req.Event)
				// 请求结束，移除缓存, 返回请求的结果
				delete(requestsMap, req.ReqId)
				switch req.Event {
				case channelEventJoinSuccess:
					var res subscribeResponse
					res.Req = &req
					writeChannel := fmt.Sprintf("%s.write", req.Prefix)
					readChannel := fmt.Sprintf("%s.read", req.Prefix)
					pubSub := r.rdb.Subscribe(context.TODO(), readChannel)
					conn := RedisConn{
						uuid:             res.Req.UUID,
						reqId:            req.ReqId,
						sessionId:        req.SessionId,
						readChannelName:  readChannel,
						writeChannelName: writeChannel,
						instructionChan:  make(chan guacd.Instruction, 100),
						cache:            r,
						pubSub:           pubSub,
						done:             make(chan struct{}),
					}
					res.conn = &conn
					go conn.run()
					responseChan <- &res
					localConnMap[conn.reqId] = &conn
				case channelEventExitSuccess:
					var res subscribeResponse
					res.Req = &req
					responseChan <- &res
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
			if msg.Id == r.ID {
				logger.Debugf("Redis cache ignore self session event %s", msg.Event.Type)
				continue
			}
			logger.Infof("Redis channel %s recv session event %s",
				redisSessionMsg.Channel, msg.Event.Type)
			r.GuaTunnelLocalCache.BroadcastSessionEvent(msg.SessionId, msg.Event)

		case req := <-r.requestChan:
			logger.Debugf("Redis cache publish request %s event %s", req.ReqId, req.Event)
			responseChan := make(chan *subscribeResponse, 1)
			r.responseChan <- responseChan
			if err := r.publishRequest(req); err != nil {
				logger.Errorf("Redis cache publish channel request err: %s", err)
				delete(requestsMap, req.ReqId)
				responseChan <- &subscribeResponse{
					Req:  req,
					err:  err,
					conn: nil,
				}
				continue
			}
			logger.Infof("Redis cache publish request %s event %s success", req.ReqId, req.Event)
			requestsMap[req.ReqId] = responseChan

		case req := <-r.reqCancelChan:
			delete(requestsMap, req.ReqId)
			logger.Debugf("Redis cache cancel request: %s", req.ReqId)

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
		r.cache.redisConExitChan <- r.reqId
		_, err := r.cache.sendRequest(&subscribeRequest{
			ReqId:     r.reqId,
			SessionId: r.sessionId,
			Event:     channelEventExit,
			Prefix:    r.reqId,
			Channel:   eventsChannel,
		})
		if err != nil {
			logger.Errorf("Redis conn %s send exit event err: %s", r.reqId, err)
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
	Req  *subscribeRequest
	err  error
	conn *RedisConn
}

type RedisGuacProxy struct {
	reqId     string
	sessionId string

	readChannelName  string
	writeChannelName string
	pubSub           *redis.PubSub

	cache *GuaTunnelRedisCache

	done chan struct{}

	tunnel *guacd.Tunnel

	once sync.Once
}

func (r *RedisGuacProxy) UUID() string {
	return r.tunnel.UUID()
}

func (r *RedisGuacProxy) run() {
	logger.Infof("Redis guacd proxy %s pubSub run", r.reqId)
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
