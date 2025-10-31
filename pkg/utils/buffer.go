package utils

import (
	"bytes"
	"sync"
)

type SyncBuffer struct {
	maxSize int
	mu      sync.Mutex
	b       bytes.Buffer
}

func (s *SyncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	if s.maxSize > 0 && s.b.Len()+len(p) > s.maxSize {
		// Discard the write if it would exceed maxSize
		s.mu.Unlock()
		return len(p), nil
	}
	n, err := s.b.Write(p)
	s.mu.Unlock()
	return n, err
}

func (s *SyncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

func NewMaxSizeBuffer(maxSize int) *SyncBuffer {
	return &SyncBuffer{
		maxSize: maxSize,
		b:       bytes.Buffer{},
	}
}
