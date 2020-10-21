package httpd

import "github.com/jumpserver/koko/pkg/logger"

const MaxMessageLen = 1024

func NewBroadcaster() *broadcaster {
	return &broadcaster{
		ttyCons:            make(map[string]*ttyCon),
		ttyEnteringChannel: make(chan *ttyCon),
		ttyLeavingChannel:  make(chan *ttyCon),

		elfinderCons:           make(map[string]*elfinderCon),
		elfinderEnterChannel:   make(chan *elfinderCon),
		elfinderLeavingChannel: make(chan *elfinderCon),

		messageChannel:        make(chan *Message, MaxMessageLen),
		checkElfinderChannel:  make(chan string),
		elfinderResultChannel: make(chan *elfinderCon),
	}
}

type broadcaster struct {
	ttyCons            map[string]*ttyCon
	ttyEnteringChannel chan *ttyCon
	ttyLeavingChannel  chan *ttyCon

	elfinderCons           map[string]*elfinderCon
	elfinderEnterChannel   chan *elfinderCon
	elfinderLeavingChannel chan *elfinderCon

	messageChannel chan *Message

	checkElfinderChannel  chan string
	elfinderResultChannel chan *elfinderCon
}

func (b *broadcaster) Start() {
	for {
		select {
		case con := <-b.ttyEnteringChannel:
			b.ttyCons[con.Uuid] = con
			logger.Infof("Ws[%s] tty enter", con.Uuid)

		case con := <-b.ttyLeavingChannel:
			delete(b.ttyCons, con.Uuid)
			logger.Infof("Ws[%s] tty leave", con.Uuid)

		case con := <-b.elfinderEnterChannel:
			b.elfinderCons[con.Uuid] = con
			logger.Infof("Ws[%s] elfinder enter", con.Uuid)

		case con := <-b.elfinderLeavingChannel:
			delete(b.elfinderCons, con.Uuid)
			logger.Infof("Ws[%s] elfinder leave", con.Uuid)

		case sid := <-b.checkElfinderChannel:
			b.elfinderResultChannel <- b.elfinderCons[sid]
		case <-b.messageChannel:
		}
	}
}

func (b *broadcaster) EnterTerminalCon(c *ttyCon) {
	b.ttyEnteringChannel <- c
}

func (b *broadcaster) LeaveTerminalCon(c *ttyCon) {
	b.ttyLeavingChannel <- c
}

func (b *broadcaster) EnterElfinderCon(c *elfinderCon) {
	b.elfinderEnterChannel <- c
}

func (b *broadcaster) LeaveElfinderCon(c *elfinderCon) {
	b.elfinderLeavingChannel <- c
}

func (b *broadcaster) Broadcast(msg *Message) {
	b.messageChannel <- msg
}

func (b *broadcaster) GetElfinderCon(sid string) *elfinderCon {
	b.checkElfinderChannel <- sid
	return <-b.elfinderResultChannel
}
