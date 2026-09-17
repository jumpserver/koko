package tunnel

import "testing"

func TestShareReadOnlyInstructions(t *testing.T) {
	conn := MonitorCon{Meta: &MetaShareUserMessage{Writable: false}}
	for _, opcode := range []string{"key", "mouse", "clipboard", "blob", "file", "pipe", "size"} {
		if conn.acceptsClientInstruction(opcode) {
			t.Fatalf("read-only share accepted %s", opcode)
		}
	}
	for _, opcode := range []string{"sync", "nop", "ack"} {
		if !conn.acceptsClientInstruction(opcode) {
			t.Fatalf("read-only share rejected %s", opcode)
		}
	}
	conn.Meta.Writable = true
	if !conn.acceptsClientInstruction("key") {
		t.Fatal("writable share rejected input")
	}
	conn.lockedStatus.Store(true)
	if conn.acceptsClientInstruction("key") {
		t.Fatal("locked share accepted input")
	}
}
