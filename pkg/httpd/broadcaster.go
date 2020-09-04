package httpd

import "github.com/jumpserver/koko/pkg/logger"

const MaxMessageLen = 1024

func NewBroadcaster() *broadcaster {
	return &broadcaster{
		ttyCons:            make(map[string]*ttyCon),
		enteringChannel:    make(chan *ttyCon),
		leavingChannel:     make(chan *ttyCon),
		messageChannel:     make(chan *Message, MaxMessageLen),
		checkChannel:       make(chan string),
		checkResultChannel: make(chan *ttyCon),
	}
}

type broadcaster struct {
	ttyCons map[string]*ttyCon

	enteringChannel chan *ttyCon
	leavingChannel  chan *ttyCon
	messageChannel  chan *Message

	checkChannel       chan string
	checkResultChannel chan *ttyCon
}

func (b *broadcaster) Start() {
	for {
		select {
		case ttyCon := <-b.enteringChannel:
			b.ttyCons[ttyCon.Uuid] = ttyCon
			logger.Infof("Ws[%s] enter", ttyCon.Uuid)
		case ttyCon := <-b.leavingChannel:
			delete(b.ttyCons, ttyCon.Uuid)
			logger.Infof("Ws[%s] leave", ttyCon.Uuid)
		case sid := <-b.checkChannel:
			b.checkResultChannel <- b.ttyCons[sid]
		case <-b.messageChannel:
		}
	}
}

func (b *broadcaster) ConEntering(c *ttyCon) {
	b.enteringChannel <- c
}

func (b *broadcaster) ConLeaving(c *ttyCon) {
	b.leavingChannel <- c
}

func (b *broadcaster) Broadcast(msg *Message) {
	b.messageChannel <- msg
}

func (b *broadcaster) GetAliveCon(sid string) *ttyCon {
	b.checkChannel <- sid
	return <-b.checkResultChannel
}
