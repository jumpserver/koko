package httpd

import "github.com/jumpserver/koko/pkg/logger"

const MaxMessageLen = 1024

func NewBroadcaster() *broadcaster {
	return &broadcaster{
		userCons:       make(map[string]*UserWebsocket),
		enterChannel:   make(chan *UserWebsocket),
		leavingChannel: make(chan *UserWebsocket),
		messageChannel: make(chan *Message, MaxMessageLen),
		checkChannel:   make(chan string),
		ResultChannel:  make(chan *UserWebsocket),
	}
}

type broadcaster struct {
	userCons       map[string]*UserWebsocket
	enterChannel   chan *UserWebsocket
	leavingChannel chan *UserWebsocket

	messageChannel chan *Message

	checkChannel  chan string
	ResultChannel chan *UserWebsocket
}

func (b *broadcaster) Start() {
	for {
		select {
		case con := <-b.enterChannel:
			b.userCons[con.Uuid] = con
			logger.Infof("Ws[%s] enter", con.Uuid)

		case con := <-b.leavingChannel:
			delete(b.userCons, con.Uuid)
			logger.Infof("Ws[%s] leave", con.Uuid)

		case sid := <-b.checkChannel:
			b.ResultChannel <- b.userCons[sid]
		case <-b.messageChannel:
		}
	}
}

func (b *broadcaster) EnterUserWebsocket(c *UserWebsocket) {
	b.enterChannel <- c
}

func (b *broadcaster) LeaveUserWebsocket(c *UserWebsocket) {
	b.leavingChannel <- c
}

func (b *broadcaster) Broadcast(msg *Message) {
	b.messageChannel <- msg
}

func (b *broadcaster) GetUserWebsocket(sid string) *UserWebsocket {
	b.checkChannel <- sid
	return <-b.ResultChannel
}
