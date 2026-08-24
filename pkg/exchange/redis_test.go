package exchange

import (
	"context"
	"fmt"
	"os"
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
	manager, err := newRedisManager(config)
	if err != nil {
		t.Fatal(err)
	}
	roomID := fmt.Sprintf("koko-redis-integration-%d", time.Now().UnixNano())
	defer func() {
		_ = manager.client.SRem(context.Background(), globalRoomsKey, roomID).Err()
		_ = manager.pubSub.Close()
		_ = manager.client.Close()
	}()
	manager.storeRoomId(roomID)
	if !manager.checkRoomExist(roomID) {
		t.Fatal("stored Redis room was not found")
	}
}
