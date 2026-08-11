package tunnel

import (
	"sync"

	"github.com/jumpserver/koko/pkg/lion/guacd"
	"github.com/jumpserver/koko/pkg/logger"
)

func NewLocalTunnelLocalCache() *GuaTunnelLocalCache {
	return &GuaTunnelLocalCache{
		Tunnels: make(map[string]*Connection),
		Rooms:   make(map[string]*Room),
	}
}

type GuaTunnelLocalCache struct {
	sync.Mutex
	Tunnels  map[string]*Connection
	roomLock sync.Mutex
	Rooms    map[string]*Room
}

func (g *GuaTunnelLocalCache) Add(t *Connection) {
	g.Lock()
	defer g.Unlock()
	g.Tunnels[t.guacdTunnel.UUID()] = t
}

func (g *GuaTunnelLocalCache) Delete(t *Connection) {
	g.Lock()
	defer g.Unlock()
	delete(g.Tunnels, t.guacdTunnel.UUID())
}

func (g *GuaTunnelLocalCache) Get(tid string) *Connection {
	g.Lock()
	defer g.Unlock()
	return g.Tunnels[tid]
}

func (g *GuaTunnelLocalCache) RangeActiveSessionIds() []string {
	g.Lock()
	ret := make([]string, 0, len(g.Tunnels))
	for i := range g.Tunnels {
		ret = append(ret, g.Tunnels[i].Sess.ID)
	}
	g.Unlock()
	return ret
}

func (g *GuaTunnelLocalCache) RangeActiveUserIds() map[string]struct{} {
	g.Lock()
	ret := make(map[string]struct{})
	for i := range g.Tunnels {
		currentUser := g.Tunnels[i].Sess.User
		ret[currentUser.ID] = struct{}{}
	}
	g.Unlock()
	return ret
}

func (g *GuaTunnelLocalCache) GetBySessionId(sid string) *Connection {
	g.Lock()
	defer g.Unlock()
	for i := range g.Tunnels {
		if sid == g.Tunnels[i].Sess.ID {
			return g.Tunnels[i]
		}
	}
	return nil
}

func (g *GuaTunnelLocalCache) GetMonitorTunnelerBySessionId(sid string) Tunneler {
	if conn := g.GetBySessionId(sid); conn != nil {
		if guacdTunnel, err := conn.CloneMonitorTunnel(); err == nil {
			return guacdTunnel
		} else {
			logger.Error(err)
		}
	}
	return nil
}

func (g *GuaTunnelLocalCache) RemoveMonitorTunneler(sid string, monitorTunnel Tunneler) {
	if conn := g.GetBySessionId(sid); conn != nil {
		if tunnel, ok := monitorTunnel.(*guacd.Tunnel); ok {
			conn.unTraceMonitorTunnel(tunnel)
		}
	}
}

func (g *GuaTunnelLocalCache) GetSessionEventChan(sid string) *EventChan {
	g.roomLock.Lock()
	defer g.roomLock.Unlock()
	room := g.Rooms[sid]
	if room == nil {
		g.Rooms[sid] = &Room{
			sid:           sid,
			eventChanMaps: make(map[string]*EventChan),
		}
	}
	return g.Rooms[sid].GetEventChannel(sid)
}

func (g *GuaTunnelLocalCache) BroadcastSessionEvent(sid string, event *Event) {
	g.roomLock.Lock()
	defer g.roomLock.Unlock()
	if room, ok := g.Rooms[sid]; ok {
		room.BroadcastEvent(event)
	}
}

func (g *GuaTunnelLocalCache) RecycleSessionEventChannel(sid string, eventChan *EventChan) {
	g.roomLock.Lock()
	defer g.roomLock.Unlock()
	if room, ok := g.Rooms[sid]; ok {
		room.RecycleEventChannel(eventChan)
		if len(room.eventChanMaps) == 0 {
			delete(g.Rooms, sid)
		}
	}
}

func (g *GuaTunnelLocalCache) GetActiveConnections() []*Connection {
	g.Lock()
	defer g.Unlock()
	ret := make([]*Connection, 0, len(g.Tunnels))
	for i := range g.Tunnels {
		ret = append(ret, g.Tunnels[i])
	}
	return ret
}
