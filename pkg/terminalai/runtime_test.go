package terminalai

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"
)

func TestExecutePTYDeadlineSendsInterrupt(t *testing.T) {
	observer, err := NewObserver(80, 24)
	if err != nil {
		t.Fatal(err)
	}
	defer observer.Close()

	var writes [][]byte
	runtime := NewRuntime(1, nil, observer, func(value []byte) {
		writes = append(writes, bytes.Clone(value))
	}, func(ChatMessage) {})
	ctx, cancel := context.WithDeadline(
		context.Background(), time.Now().Add(-time.Second),
	)
	defer cancel()

	_, _, err = runtime.execute(ctx, CommandProposal{
		Command: "du /", Execution: ExecutionPTY,
	}, nil)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("execute error = %v, want deadline exceeded", err)
	}
	if len(writes) != 2 || !bytes.Equal(writes[0], []byte("du /\r")) ||
		!bytes.Equal(writes[1], []byte{3}) {
		t.Fatalf("PTY writes = %q, want command followed by Ctrl+C", writes)
	}
}
