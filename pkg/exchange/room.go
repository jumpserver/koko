package exchange

import (
	"container/ring"
	"encoding/json"
	"io"
	"sync"
	"time"

	"github.com/jumpserver/koko/pkg/common"
	"github.com/jumpserver/koko/pkg/logger"
)

const roomSubscriberBufferSize = 32

type RoomManager interface {
	Add(s *Room)
	Delete(s *Room)
	Get(sid string) *Room
	Close() error
}

var (
	_ RoomManager = (*localRoomManager)(nil)
	_ RoomManager = (*redisRoomManager)(nil)
)

func CreateRoom(id string, inChan chan *RoomMessage) *Room {
	s := &Room{
		Id:             id,
		userInputChan:  inChan,
		broadcastChan:  make(chan *RoomMessage),
		subscriber:     make(chan *Conn),
		unSubscriber:   make(chan *Conn),
		done:           make(chan struct{}),
		recentMessages: ring.New(5),
	}
	return s
}

type Room struct {
	Id string

	userInputChan chan *RoomMessage
	forwardEvents chan *RoomMessage

	broadcastChan chan *RoomMessage

	subscriber chan *Conn

	unSubscriber chan *Conn

	done chan struct{}

	once sync.Once

	recentMessages *ring.Ring
}

func (r *Room) run() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	defer r.closeOnce()
	connMaps := make(map[string]*roomSubscriber)
	pending := make(map[string]*roomSubscriber)
	ready := make(chan struct{}, 1)
	var pendingMessage *RoomMessage
	currentOnlineUsers := make(map[string]MetaMessage)
	var ZMODEMStatus bool
	for {
		broadcast := r.broadcastChan
		if len(pending) > 0 {
			// Apply backpressure at the room input while keeping subscription
			// changes and shutdown responsive. Retain only one broadcast.
			broadcast = nil
		} else {
			pendingMessage = nil
		}
		select {
		case <-ready:
			for id, subscriber := range pending {
				if subscriber.send(pendingMessage) {
					delete(pending, id)
				}
			}
		case <-ticker.C:
			if len(connMaps) == 0 {
				logger.Infof("Room %s has no connection now and exit", r.Id)
				return
			}
		case con := <-r.subscriber:
			if subscriber := connMaps[con.Id]; subscriber != nil {
				delete(pending, con.Id)
				subscriber.stop(false)
			}
			subscriber := newRoomSubscriber(con, ready)
			connMaps[con.Id] = subscriber
			go subscriber.run()
			if ZMODEMStatus {
				subscriber.send(&RoomMessage{
					Event: ActionEvent,
					Body:  []byte(ZmodemStartEvent),
				})
			}
			r.recentMessages.Do(func(value interface{}) {
				if msg, ok := value.(*RoomMessage); ok {
					switch msg.Event {
					case DataEvent:
						subscriber.send(msg)
					}
				}
			})
			body, _ := json.Marshal(currentOnlineUsers)
			subscriber.send(&RoomMessage{
				Event: ShareUsers,
				Body:  body,
			})
			logger.Debugf("Room %s current connections count: %d", r.Id, len(connMaps))
		case con := <-r.unSubscriber:
			if subscriber := connMaps[con.Id]; subscriber != nil && subscriber.conn == con {
				delete(connMaps, con.Id)
				delete(pending, con.Id)
				subscriber.stop(false)
			}
			logger.Debugf("Room %s current connections count: %d", r.Id, len(connMaps))
		case msg := <-broadcast:
			switch msg.Event {
			case DataEvent:
				r.recentMessages.Value = msg
				r.recentMessages = r.recentMessages.Next()
			case ShareJoin:
				key := msg.Meta.User + msg.Meta.Created
				currentOnlineUsers[key] = msg.Meta
			case ShareLeave:
				key := msg.Meta.User + msg.Meta.Created
				delete(currentOnlineUsers, key)
			case ShareUsers:
				var users map[string]MetaMessage
				if err := json.Unmarshal(msg.Body, &users); err == nil {
					if users == nil {
						users = make(map[string]MetaMessage)
					}
					currentOnlineUsers = users
				}
			case ActionEvent:
				switch string(msg.Body) {
				case ZmodemStartEvent:
					ZMODEMStatus = true
				case ZmodemEndEvent:
					ZMODEMStatus = false
				default:
					ZMODEMStatus = false
				}
			}
			pendingMessage = msg
			r.broadcastMessage(connMaps, pending, msg)

		case <-r.done:
			for _, subscriber := range connMaps {
				subscriber.stop(true)
			}
			return
		}
	}
}

func (r *Room) Subscribe(conn *Conn) {
	select {
	case <-r.done:
	case r.subscriber <- conn:
	}
}

func (r *Room) UnSubscribe(conn *Conn) {
	select {
	case <-r.done:
	case r.unSubscriber <- conn:
	}
}

func (r *Room) Broadcast(msg *RoomMessage) {
	if r.forwardEvents != nil && (msg.Event == ShareJoin || msg.Event == ShareLeave) {
		select {
		case <-r.done:
		case r.forwardEvents <- msg:
		}
		return
	}
	r.broadcast(msg)
}

// BroadcastChan lets the session owner wait for output capacity in its event
// loop while still handling termination and connection closure.
func (r *Room) BroadcastChan() chan<- *RoomMessage {
	return r.broadcastChan
}

func (r *Room) broadcast(msg *RoomMessage) {
	select {
	case <-r.done:
	case r.broadcastChan <- msg:
	}
}

func (r *Room) Receive(msg *RoomMessage) {
	select {
	case <-r.done:
	case r.userInputChan <- msg:
	}
}

func (r *Room) broadcastMessage(conns, pending map[string]*roomSubscriber, msg *RoomMessage) {
	for id, subscriber := range conns {
		if subscriber.send(msg) {
			continue
		}
		select {
		case <-r.done:
			return
		default:
		}
		if subscriber.conn.Primary {
			pending[id] = subscriber
			continue
		}
		delete(conns, id)
		subscriber.stop(true)
		logger.Errorf("Room %s close slow connection %s (primary=%t)", r.Id, id, subscriber.conn.Primary)
	}
}

func (r *Room) Done() <-chan struct{} {
	return r.done
}

func (r *Room) stop() {
	r.closeOnce()
}

func (r *Room) closeOnce() {
	r.once.Do(func() {
		close(r.done)
	})
}

func WrapperUserCon(stream Stream) *Conn {
	return &Conn{
		Id:     common.UUID(),
		Stream: stream,
	}
}

type Stream interface {
	io.WriteCloser
	HandleRoomEvent(event string, msg *RoomMessage)
}

type Conn struct {
	Id string
	Stream
	// Primary connections apply backpressure instead of being evicted on queue overflow.
	Primary bool
}

type roomSubscriber struct {
	conn     *Conn
	messages chan *RoomMessage
	done     chan struct{}
	ready    chan<- struct{}
	once     sync.Once
}

func newRoomSubscriber(conn *Conn, ready chan<- struct{}) *roomSubscriber {
	return &roomSubscriber{
		conn:     conn,
		messages: make(chan *RoomMessage, roomSubscriberBufferSize),
		done:     make(chan struct{}),
		ready:    ready,
	}
}

func (s *roomSubscriber) run() {
	for {
		select {
		case <-s.done:
			return
		default:
		}
		select {
		case <-s.done:
			return
		case msg := <-s.messages:
			if s.conn.Primary {
				select {
				case s.ready <- struct{}{}:
				default:
				}
			}
			s.conn.handlerMessage(msg)
		}
	}
}

func (s *roomSubscriber) send(msg *RoomMessage) bool {
	select {
	case <-s.done:
		return false
	default:
	}
	select {
	case s.messages <- msg:
		return true
	default:
		return false
	}
}

func (s *roomSubscriber) stop(closeConn bool) {
	s.once.Do(func() {
		close(s.done)
		if closeConn {
			_ = s.conn.Close()
		}
	})
}

func (c *Conn) handlerMessage(msg *RoomMessage) {
	switch msg.Event {
	case DataEvent:
		_, _ = c.Write(msg.Body)
	case PingEvent:
		_, _ = c.Write(nil)
	default:
		c.HandleRoomEvent(msg.Event, msg)
	}
}
