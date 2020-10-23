package httpd

import "github.com/jumpserver/koko/pkg/logger"

const MaxMessageLen = 1024

func NewBroadcaster() *broadcaster {
	return &broadcaster{
		userConns:      make(map[string]*UserWebsocket),
		enterChannel:   make(chan *UserWebsocket),
		leavingChannel: make(chan *UserWebsocket),
		messageChannel: make(chan *Message, MaxMessageLen),
		checkChannel:   make(chan string),
		resultChannel:  make(chan *UserWebsocket),
	}
}

type broadcaster struct {
	userConns      map[string]*UserWebsocket
	enterChannel   chan *UserWebsocket
	leavingChannel chan *UserWebsocket

	messageChannel chan *Message

	checkChannel  chan string
	resultChannel chan *UserWebsocket
}

func (b *broadcaster) Start() {
	for {
		select {
		case conn := <-b.enterChannel:
			b.userConns[conn.Uuid] = conn
			logger.Infof("Ws[%s] enter", conn.Uuid)

		case conn := <-b.leavingChannel:
			delete(b.userConns, conn.Uuid)
			logger.Infof("Ws[%s] leave", conn.Uuid)

		case sid := <-b.checkChannel:
			b.resultChannel <- b.userConns[sid]
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
	return <-b.resultChannel
}
