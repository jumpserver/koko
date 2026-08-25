package exchange

import "sync"

func newLocalManager() *localRoomManager {
	m := localRoomManager{newLocalCache()}
	return &m
}

type localRoomManager struct {
	*localCache
}

func (m *localRoomManager) Close() error {
	m.localCache.Close()
	return nil
}

func newLocalCache() *localCache {
	return &localCache{caches: make(map[string]*Room)}
}

type localCache struct {
	mu     sync.RWMutex
	caches map[string]*Room
}

func (l *localCache) Add(s *Room) {
	l.AddIfAbsent(s)
}

func (l *localCache) AddIfAbsent(s *Room) (*Room, bool) {
	l.mu.Lock()
	if room := l.caches[s.Id]; room != nil {
		l.mu.Unlock()
		return room, false
	}
	l.caches[s.Id] = s
	l.mu.Unlock()
	go s.run()
	return s, true
}

func (l *localCache) Delete(s *Room) {
	l.DeleteIf(s)
}

func (l *localCache) DeleteIf(s *Room) bool {
	l.mu.Lock()
	if l.caches[s.Id] != s {
		l.mu.Unlock()
		return false
	}
	delete(l.caches, s.Id)
	l.mu.Unlock()
	s.stop()
	return true
}

func (l *localCache) Get(sid string) *Room {
	l.mu.RLock()
	room := l.caches[sid]
	l.mu.RUnlock()
	return room
}

func (l *localCache) Rooms() []*Room {
	l.mu.RLock()
	rooms := make([]*Room, 0, len(l.caches))
	for _, room := range l.caches {
		rooms = append(rooms, room)
	}
	l.mu.RUnlock()
	return rooms
}

func (l *localCache) Close() {
	_ = l.CloseRooms()
}

func (l *localCache) CloseRooms() []*Room {
	l.mu.Lock()
	rooms := make([]*Room, 0, len(l.caches))
	for _, room := range l.caches {
		rooms = append(rooms, room)
	}
	l.caches = make(map[string]*Room)
	l.mu.Unlock()
	for _, room := range rooms {
		room.stop()
	}
	return rooms
}
