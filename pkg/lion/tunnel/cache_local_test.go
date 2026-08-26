package tunnel

import "testing"

func TestLocalCacheRecyclesSessionEventChannel(t *testing.T) {
	cache := NewLocalTunnelLocalCache()
	eventChan := cache.GetSessionEventChan("session")

	cache.RecycleSessionEventChannel("session", eventChan)

	if _, ok := cache.Rooms["session"]; ok {
		t.Fatal("empty session room was not removed")
	}
	if _, ok := <-eventChan.eventCh; ok {
		t.Fatal("recycled session event channel is still open")
	}
}
