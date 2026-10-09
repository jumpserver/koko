package cache

import (
	"sync"
	"testing"
	"time"
)

func TestGCConcurrentMutation(t *testing.T) {
	c := &ConnectTokenCache{data: make(map[string]*ConnectTokenItem)}
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < 10000; i++ {
			c.lock.Lock()
			c.data["expired"] = &ConnectTokenItem{maxExpiredTime: time.Now().Add(-time.Hour).Unix()}
			c.lock.Unlock()
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 10000; i++ {
			c.GC()
		}
	}()
	wg.Wait()
	c.GC()
	if len(c.data) != 0 {
		t.Fatal("expired entry was not collected")
	}
}
