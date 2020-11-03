package exchange

func newLocalManager() *localRoomManager {
	l := &localRoomManager{
		caches:     make(map[string]*Room),
		storeChan:  make(chan *Room),
		leaveChan:  make(chan *Room),
		checkChan:  make(chan string),
		resultChan: make(chan *Room),
	}
	go l.run()
	return l

}

type localRoomManager struct {
	caches map[string]*Room

	storeChan chan *Room

	leaveChan chan *Room

	checkChan chan string

	resultChan chan *Room
}

func (l *localRoomManager) run() {
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

func (l localRoomManager) Add(s *Room) {
	l.storeChan <- s
}

func (l localRoomManager) Delete(s *Room) {
	l.leaveChan <- s
}

func (l localRoomManager) Get(sid string) *Room {
	l.checkChan <- sid
	return <-l.resultChan
}
