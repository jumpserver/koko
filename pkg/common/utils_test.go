package common

import (
	"bytes"
	"errors"
	"io"
	"testing"
)

type shortReaderAt struct{}

func (shortReaderAt) ReadAt(p []byte, _ int64) (int, error) {
	copy(p, "x")
	return 1, io.EOF
}

type shortWriterAt struct{}

func (shortWriterAt) WriteAt(p []byte, _ int64) (int, error) {
	return len(p) - 1, nil
}

type discardWriterAt struct{}

func (discardWriterAt) WriteAt(p []byte, _ int64) (int, error) {
	return len(p), nil
}

func TestChunkedFileTransferRejectsShortRead(t *testing.T) {
	err := ChunkedFileTransfer(discardWriterAt{}, shortReaderAt{}, 0, 2)
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("expected unexpected EOF, got %v", err)
	}
}

func TestChunkedFileTransferRejectsShortWrite(t *testing.T) {
	err := ChunkedFileTransfer(shortWriterAt{}, bytes.NewReader([]byte("ok")), 0, 2)
	if !errors.Is(err, io.ErrShortWrite) {
		t.Fatalf("expected short write, got %v", err)
	}
}
