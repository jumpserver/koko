package tunnel

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/jumpserver/koko/pkg/lion/guacd"
)

type closingReader struct {
	*bytes.Reader
	closed bool
}

func (r *closingReader) Close() error {
	r.closed = true
	return nil
}

func TestInputStreamFailureAckFinishesStream(t *testing.T) {
	reader := &closingReader{Reader: bytes.NewReader(nil)}
	stream := &InputStreamResource{streamIndex: "1", reader: reader, done: make(chan struct{})}
	filter := &InputStreamInterceptingFilter{streams: map[string]*InputStreamResource{"1": stream}}

	filter.handleAck(&guacd.Instruction{Args: []string{"1", "", "1"}})

	if err := stream.Wait(context.Background()); err == nil {
		t.Fatal("expected failed acknowledgement")
	}
	if !reader.closed {
		t.Fatal("expected failed stream reader to close")
	}
}

func TestOutputStreamFinishKeepsReplacement(t *testing.T) {
	old := &OutStreamResource{streamIndex: "1", done: make(chan struct{}), ctx: context.Background()}
	current := &OutStreamResource{streamIndex: "1", done: make(chan struct{}), ctx: context.Background()}
	filter := &OutputStreamInterceptingFilter{streams: map[string]*OutStreamResource{"1": current}}
	errExpected := errors.New("write failed")

	filter.finishOutStream("1", old, errExpected)

	if filter.streams["1"] != current {
		t.Fatal("finishing a replaced stream removed its replacement")
	}
	if !errors.Is(old.Wait(), errExpected) {
		t.Fatal("expected output stream error")
	}
}
