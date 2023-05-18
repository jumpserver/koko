package exchange

func newLocalManager() *localRoomManager {
	m := localRoomManager{newLocalCache()}
	return &m
}

type localRoomManager struct {
	*localCache
}

func newLocalCache() *localCache {
	l := &localCache{
		caches:     make(map[string]*Room),
		storeChan:  make(chan *Room),
		leaveChan:  make(chan *Room),
		checkChan:  make(chan string),
		resultChan: make(chan *Room),
	}
	go l.run()
	return l

}

type localCache struct {
	caches map[string]*Room

	storeChan chan *Room

	leaveChan chan *Room

	checkChan chan string

	resultChan chan *Room
}

func (l *localCache) run() {
	for {
		select {
		case s := <-l.storeChan:
			l.caches[s.Id] = s
			go s.run()
		case s := <-l.leaveChan:
			delete(l.caches, s.Id)
			s.stop()
		case sid := <-l.checkChan:
			l.resultChan <- l.caches[sid]
		}
	}
}

func (l *localCache) Add(s *Room) {
	l.storeChan <- s
}

func (l *localCache) Delete(s *Room) {
	l.leaveChan <- s
}

func (l *localCache) Get(sid string) *Room {
	l.checkChan <- sid
	return <-l.resultChan
}
