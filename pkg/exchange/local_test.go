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
	fast := &recordingRoomStream{writes: make(chan []byte, 1)}
	room.Subscribe(WrapperUserCon(slow))
	room.Subscribe(WrapperUserCon(fast))

	done := make(chan struct{})
	go func() {
		room.Broadcast(&RoomMessage{Event: DataEvent, Body: []byte("data")})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("slow subscriber blocked room broadcast")
	}
	select {
	case data := <-fast.writes:
		if string(data) != "data" {
			t.Fatalf("unexpected data: %q", data)
		}
	case <-time.After(time.Second):
		t.Fatal("fast subscriber did not receive room data")
	}
	_ = slow.Close()
}

type recordingRoomStream struct {
	writes chan []byte
}

func (s *recordingRoomStream) Write(p []byte) (int, error) {
	s.writes <- append([]byte(nil), p...)
	return len(p), nil
}

func (s *recordingRoomStream) Close() error {
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
