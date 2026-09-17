package common

import "testing"

func TestParseRedisClusterSlotsWithMetadata(t *testing.T) {
	value := []interface{}{[]interface{}{
		int64(0), int64(16383),
		[]interface{}{"127.0.0.1", int64(6379), "node-id", []interface{}{}},
	}}
	slots, err := parseRedisClusterSlots(value)
	if err != nil {
		t.Fatal(err)
	}
	if len(slots) != 1 || len(slots[0].Nodes) != 1 ||
		slots[0].Nodes[0].Addr != "127.0.0.1:6379" {
		t.Fatalf("unexpected Redis cluster slots: %#v", slots)
	}
}
