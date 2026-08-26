package tunnel

import (
	"sync"

	"github.com/jumpserver-dev/sdk-go/common"

	"github.com/jumpserver/koko/pkg/lion/guacd"
	"github.com/jumpserver/koko/pkg/logger"
)

type Tunneler interface {
	UUID() string
	WriteAndFlush(p []byte) (int, error)
	ReadInstruction() (guacd.Instruction, error)
	Close() error
}

type GuaTunnelCache interface {
	Add(*Connection)
	Delete(*Connection)
	Get(string) *Connection
	RangeActiveSessionIds() []string
	RangeActiveUserIds() map[string]struct{}
	GetBySessionId(sid string) *Connection
	GetMonitorTunnelerBySessionId(sid string) Tunneler
	RemoveMonitorTunneler(sid string, monitorTunnel Tunneler)

	GetSessionEventChan(sid string) *EventChan
	BroadcastSessionEvent(sid string, event *Event)
	RecycleSessionEventChannel(sid string, eventChan *EventChan)

	GetActiveConnections() []*Connection
}

type SessionEvent interface {
	GetSessionEventChannel(sid string) *EventChan
	RecycleSessionEventChannel(sid string, eventChan *EventChan)
	BroadcastSessionEvent(sid string, event *Event)
}

var (
	_ GuaTunnelCache = (*GuaTunnelLocalCache)(nil)
	_ GuaTunnelCache = (*GuaTunnelRedisCache)(nil)
)

type GuaTunnelCacheManager struct {
	GuaTunnelCache
}

type Room struct {
	sid           string
	eventChanMaps map[string]*EventChan
	lock          sync.Mutex
}

func (r *Room) GetEventChannel(sid string) *EventChan {
	r.lock.Lock()
	defer r.lock.Unlock()
	eventChan := NewEventChan(sid)
	r.eventChanMaps[eventChan.id] = eventChan
	return eventChan
}

func (r *Room) RecycleEventChannel(eventChan *EventChan) {
	r.lock.Lock()
	defer r.lock.Unlock()
	delete(r.eventChanMaps, eventChan.id)
	eventChan.Close()
}

func (r *Room) BroadcastEvent(event *Event) {
	r.lock.Lock()
	defer r.lock.Unlock()
	for _, eventChan := range r.eventChanMaps {
		eventChan.SendEvent(event)
	}
}

type EventChan struct {
	id      string
	sid     string
	eventCh chan *Event
}

func (e *EventChan) GetEventChannel() chan *Event {
	return e.eventCh
}

func (e *EventChan) SendEvent(event *Event) {
	select {
	case e.eventCh <- event:
	default:
		logger.Errorf("EventChan %s for session %s is full", e.id, e.sid)
	}
}

func (e *EventChan) Close() {
	close(e.eventCh)
}

func NewEventChan(sid string) *EventChan {
	return &EventChan{
		id:      common.UUID(),
		sid:     sid,
		eventCh: make(chan *Event, 32),
	}
}

type Event struct {
	Type string
	Data []byte
}

const (
	ShareJoin  = "share_join"
	ShareExit  = "share_exit"
	ShareUsers = "share_users"

	ShareRemoveUser    = "share_remove_user"
	ShareSessionPause  = "share_session_pause"
	ShareSessionResume = "share_session_resume"

	PermExpiredEvent = "perm_expired"
	PermValidEvent   = "perm_valid"
)
