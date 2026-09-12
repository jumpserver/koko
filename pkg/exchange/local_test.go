package exchange

import (
	"errors"
	"io"
	"sync"
	"testing"
	"time"
)

func TestLocalCacheDeleteIf(t *testing.T) {
	cache := newLocalCache()
	room := CreateRoom("room", make(chan *RoomMessage))
	other := CreateRoom("room", make(chan *RoomMessage))
	cache.Add(room)

	if cache.DeleteIf(other) {
		t.Fatal("deleted a replacement room")
	}
	if cache.Get(room.Id) != room {
		t.Fatal("cached room was replaced")
	}
	if !cache.DeleteIf(room) {
		t.Fatal("cached room was not deleted")
	}
}

func TestRedisUserConStateIsIdempotent(t *testing.T) {
	state := &redisUserConState{subscribers: make(map[string]struct{})}
	req := &subscribeRequest{ManagerId: "koko"}
	if !state.add(req) || state.add(req) || state.count() != 1 {
		t.Fatal("duplicate join changed subscriber count")
	}
	if !state.remove(req) || state.remove(req) || state.count() != 0 {
		t.Fatal("duplicate leave changed subscriber count")
	}
}

func TestSendRequestAfterManagerClosed(t *testing.T) {
	manager := &redisRoomManager{
		reqChan: make(chan *managerRequest),
		done:    make(chan struct{}),
	}
	manager.markDone()
	_, err := manager.sendRequest(&subscribeRequest{Event: JoinEvent})
	if !errors.Is(err, errRedisManagerClosed) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSlowSubscriberDoesNotBlockRoom(t *testing.T) {
	room := CreateRoom("room", make(chan *RoomMessage))
	go room.run()
	defer room.stop()

	slow := &blockingRoomStream{closed: make(chan struct{})}
	defer slow.Close()
	fast := &recordingRoomStream{writes: make(chan []byte, 1)}
	room.Subscribe(WrapperUserCon(slow))
	primary := WrapperUserCon(fast)
	primary.Primary = true
	room.Subscribe(primary)

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < roomSubscriberBufferSize+2; i++ {
			room.Broadcast(&RoomMessage{Event: DataEvent, Body: []byte("data")})
			select {
			case <-fast.writes:
			case <-room.Done():
				return
			}
		}
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("slow subscriber blocked room broadcast")
	}
	select {
	case <-slow.closed:
	default:
		t.Fatal("slow subscriber was not evicted")
	}
}

func TestPrimaryBackpressureKeepsRoomResponsive(t *testing.T) {
	room := CreateRoom("room", nil)
	go room.run()
	defer room.stop()
	stream := &recordingRoomStream{writes: make(chan []byte), closed: make(chan struct{})}
	defer stream.Close()
	conn := WrapperUserCon(stream)
	conn.Primary = true
	room.Subscribe(conn)
	observer := &recordingRoomStream{writes: make(chan []byte, roomSubscriberBufferSize+3)}
	room.Subscribe(WrapperUserCon(observer))

	sent := make(chan struct{})
	filled := make(chan struct{})
	go func() {
		defer close(sent)
		for i := 0; i < roomSubscriberBufferSize+3; i++ {
			room.Broadcast(&RoomMessage{Event: DataEvent, Body: []byte{byte(i)}})
			select {
			case <-observer.writes:
			case <-room.Done():
				return
			}
			if i == roomSubscriberBufferSize+1 {
				close(filled)
			}
		}
	}()
	// One write is in flight, 32 messages are queued, and one broadcast is pending.
	// The observer must receive that pending broadcast even while the primary stalls.
	select {
	case <-filled:
	case <-time.After(time.Second):
		t.Fatal("primary queue blocked delivery to the observer")
	}
	select {
	case <-sent:
		t.Fatal("full primary queue did not wait for the consumer")
	case <-time.After(1100 * time.Millisecond):
	}
	changed := make(chan struct{})
	go func() {
		other := WrapperUserCon(&recordingRoomStream{writes: make(chan []byte, 5)})
		room.Subscribe(other)
		room.UnSubscribe(other)
		close(changed)
	}()
	select {
	case <-changed:
	case <-time.After(time.Second):
		t.Fatal("primary queue blocked subscription changes")
	}
	for i := 0; i < roomSubscriberBufferSize+3; i++ {
		select {
		case data := <-stream.writes:
			if len(data) != 1 || data[0] != byte(i) {
				t.Fatalf("output %d was lost or reordered: %v", i, data)
			}
		case <-time.After(time.Second):
			t.Fatal("primary subscriber lost output")
		}
	}
	select {
	case <-sent:
	case <-time.After(time.Second):
		t.Fatal("broadcast did not resume after the primary consumer recovered")
	}
}

func TestPrimaryBackpressureCancellation(t *testing.T) {
	for _, action := range []string{"unsubscribe", "stop"} {
		t.Run(action, func(t *testing.T) {
			room := CreateRoom("room", nil)
			go room.run()
			defer room.stop()
			stream := &blockingRoomStream{closed: make(chan struct{})}
			defer stream.Close()
			conn := WrapperUserCon(stream)
			conn.Primary = true
			room.Subscribe(conn)
			msg := &RoomMessage{Event: DataEvent, Body: []byte("data")}
			sent := make(chan struct{})
			go func() {
				defer close(sent)
				for i := 0; i < roomSubscriberBufferSize+3; i++ {
					room.Broadcast(msg)
				}
			}()
			select {
			case <-sent:
				t.Fatal("full primary queue did not apply backpressure")
			case <-time.After(20 * time.Millisecond):
			}
			unsubscribed := make(chan struct{})
			go func() {
				if action == "stop" {
					room.stop()
				}
				room.UnSubscribe(conn)
				close(unsubscribed)
			}()
			select {
			case <-unsubscribed:
			case <-time.After(time.Second):
				t.Fatal("primary queue blocked unsubscription")
			}
			select {
			case <-sent:
			case <-time.After(time.Second):
				t.Fatal("queue wait was not cancelled")
			}
		})
	}
}

type recordingRoomStream struct {
	writes chan []byte
	closed chan struct{}
	once   sync.Once
}

func (s *recordingRoomStream) Write(p []byte) (int, error) {
	select {
	case s.writes <- append([]byte(nil), p...):
		return len(p), nil
	case <-s.closed:
		return 0, io.ErrClosedPipe
	}
}

func (s *recordingRoomStream) Close() error {
	if s.closed != nil {
		s.once.Do(func() { close(s.closed) })
	}
	return nil
}

func (s *recordingRoomStream) HandleRoomEvent(string, *RoomMessage) {}

type blockingRoomStream struct {
	closed chan struct{}
	once   sync.Once
}

func (s *blockingRoomStream) Write(p []byte) (int, error) {
	<-s.closed
	return 0, io.ErrClosedPipe
}

func (s *blockingRoomStream) Close() error {
	s.once.Do(func() {
		close(s.closed)
	})
	return nil
}

func (s *blockingRoomStream) HandleRoomEvent(string, *RoomMessage) {}
