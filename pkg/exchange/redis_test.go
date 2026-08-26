package exchange

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"
)

func TestRedisManagerIntegration(t *testing.T) {
	address := os.Getenv("KOKO_REDIS_TEST_ADDR")
	if address == "" {
		t.Skip("KOKO_REDIS_TEST_ADDR is not set")
	}
	testRedisManagerIntegration(t, Config{Addr: address})
}

func TestRedisClusterManagerIntegration(t *testing.T) {
	address := os.Getenv("KOKO_REDIS_CLUSTER_TEST_ADDR")
	if address == "" {
		t.Skip("KOKO_REDIS_CLUSTER_TEST_ADDR is not set")
	}
	testRedisManagerIntegration(t, Config{Clusters: []string{address}})
}

func TestRedisSentinelManagerIntegration(t *testing.T) {
	address := os.Getenv("KOKO_REDIS_SENTINEL_TEST_ADDR")
	if address == "" {
		t.Skip("KOKO_REDIS_SENTINEL_TEST_ADDR is not set")
	}
	testRedisManagerIntegration(t, Config{
		SentinelsHost: "mymaster/" + address,
	})
}

func testRedisManagerIntegration(t *testing.T, config Config) {
	config.DialTimeout = 5 * time.Second
	source, err := newRedisManager(config)
	if err != nil {
		t.Fatal(err)
	}
	remote, err := newRedisManager(config)
	if err != nil {
		_ = source.Close()
		t.Fatal(err)
	}
	roomID := fmt.Sprintf("koko-redis-integration-%d", time.Now().UnixNano())
	defer func() {
		_ = source.client.SRem(context.Background(), globalRoomsKey, roomID).Err()
		_ = source.client.HDel(context.Background(), roomOwnersKey, roomID).Err()
		_ = remote.Close()
		_ = source.Close()
	}()

	sourceRoom := CreateRoom(roomID, make(chan *RoomMessage))
	source.Add(sourceRoom)
	sourceStream := &redisTestStream{
		writes: make(chan []byte, 1),
		events: make(chan string, 2),
	}
	sourceConn := WrapperUserCon(sourceStream)
	sourceRoom.Subscribe(sourceConn)
	defer sourceRoom.UnSubscribe(sourceConn)
	if !remote.checkRoomExist(roomID) {
		t.Fatal("stored Redis room was not found")
	}
	staleRoomID := roomID + "-stale"
	if err := source.client.SAdd(context.Background(), globalRoomsKey, staleRoomID).Err(); err != nil {
		t.Fatal(err)
	}
	if err := source.client.HSet(context.Background(), roomOwnersKey, staleRoomID, "stale-manager").Err(); err != nil {
		t.Fatal(err)
	}
	if remote.checkRoomExist(staleRoomID) {
		t.Fatal("room with an expired owner lease still exists")
	}
	sourceRoom.Broadcast(&RoomMessage{Event: DataEvent, Body: []byte("prompt")})

	const joins = 4
	rooms := make(chan *Room, joins)
	var wg sync.WaitGroup
	for range joins {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rooms <- remote.Get(roomID)
		}()
	}
	wg.Wait()
	close(rooms)
	var remoteRoom *Room
	for room := range rooms {
		if room == nil {
			t.Fatal("remote room was not created")
		}
		if remoteRoom != nil && remoteRoom != room {
			t.Fatal("concurrent joins created different rooms")
		}
		remoteRoom = room
	}

	stream := &redisTestStream{writes: make(chan []byte, 1)}
	conn := WrapperUserCon(stream)
	remoteRoom.Subscribe(conn)
	defer remoteRoom.UnSubscribe(conn)
	select {
	case data := <-stream.writes:
		if string(data) != "prompt" {
			t.Fatalf("unexpected replay: %q", data)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("initial room output was lost")
	}
	remoteRoom.Broadcast(&RoomMessage{
		Event: ShareJoin,
		Meta:  MetaMessage{User: "remote", Created: "now"},
	})
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	for {
		select {
		case event := <-sourceStream.events:
			if event == ShareJoin {
				source.Delete(sourceRoom)
				return
			}
		case <-timer.C:
			t.Fatal("remote share presence was not forwarded to source room")
		}
	}
}

type redisTestStream struct {
	writes chan []byte
	events chan string
}

func (s *redisTestStream) Write(p []byte) (int, error) {
	s.writes <- append([]byte(nil), p...)
	return len(p), nil
}

func (s *redisTestStream) Close() error {
	return nil
}

func (s *redisTestStream) HandleRoomEvent(event string, _ *RoomMessage) {
	if s.events != nil {
		s.events <- event
	}
}
