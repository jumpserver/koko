package httpd

import (
	"bytes"
	"runtime"
	"testing"
)

func TestEncodeTerminalOutputEnvelope(t *testing.T) {
	msg := Message{Type: TerminalBinary, TerminalId: 7, Raw: []byte("terminal output")}
	encoded, err := encodeMessageEnvelope(&msg)
	if err != nil {
		t.Fatal(err)
	}
	frame, err := parseEnvelope(encoded)
	if err != nil {
		t.Fatal(err)
	}
	terminalID, data, err := parseTerminalEnvelopePayload(frame.Payload)
	if err != nil {
		t.Fatal(err)
	}
	if frame.Type != envelopeTerminalOutput || terminalID != msg.TerminalId || !bytes.Equal(data, msg.Raw) {
		t.Fatalf("unexpected terminal output envelope: type=%d terminalID=%d data=%q", frame.Type, terminalID, data)
	}

	var result []byte
	allocs := testing.AllocsPerRun(100, func() {
		result, _ = encodeMessageEnvelope(&msg)
	})
	runtime.KeepAlive(result)
	if allocs != 1 {
		t.Fatalf("terminal output envelope allocations = %v, want 1", allocs)
	}
}
