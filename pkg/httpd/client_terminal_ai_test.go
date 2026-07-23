package httpd

import (
	"bytes"
	"testing"
)

type testWriteCloser struct {
	bytes.Buffer
}

func (w *testWriteCloser) Close() error {
	return nil
}

func TestTerminalAIInputLock(t *testing.T) {
	writer := &testWriteCloser{}
	client := &Client{UserWrite: writer}
	client.SetInputLocked(true)
	client.WriteData([]byte("manual"))
	if writer.Len() != 0 {
		t.Fatal("manual input passed while the terminal AI input lock was active")
	}
	client.WriteAgentData([]byte("agent"))
	if writer.String() != "agent" {
		t.Fatalf("agent input = %q", writer.String())
	}
	client.SetInputLocked(false)
	client.WriteData([]byte("-manual"))
	if writer.String() != "agent-manual" {
		t.Fatalf("unlocked input = %q", writer.String())
	}
}
