package utils

import (
	"bytes"
	"sync"
)

// Buffer pool sizes for different use cases
const (
	SmallBufferSize  = 1024       // 1KB for small reads
	MediumBufferSize = 32 * 1024  // 32KB for medium reads
	LargeBufferSize  = 64 * 1024  // 64KB for large reads
)

// BufferPool provides a sync.Pool based buffer pool with configurable size
type BufferPool struct {
	pool sync.Pool
	size int
}

// NewBufferPool creates a new buffer pool with the specified buffer size
func NewBufferPool(size int) *BufferPool {
	return &BufferPool{
		size: size,
		pool: sync.Pool{
			New: func() interface{} {
				buf := make([]byte, size)
				return &buf
			},
		},
	}
}

// Get returns a buffer from the pool
func (p *BufferPool) Get() *[]byte {
	return p.pool.Get().(*[]byte)
}

// Put returns a buffer to the pool
func (p *BufferPool) Put(buf *[]byte) {
	if buf == nil {
		return
	}
	// Only return buffers of the expected size to the pool
	if cap(*buf) == p.size {
		*buf = (*buf)[:p.size]
		p.pool.Put(buf)
	}
}

// Global buffer pools for common use cases
var (
	// SmallBufferPool for small reads (1KB)
	SmallBufferPool = NewBufferPool(SmallBufferSize)
	// MediumBufferPool for medium reads (32KB)
	MediumBufferPool = NewBufferPool(MediumBufferSize)
	// LargeBufferPool for large reads (64KB)
	LargeBufferPool = NewBufferPool(LargeBufferSize)
)

// BytesBufferPool provides a sync.Pool for bytes.Buffer instances.
// It helps reduce memory allocations by reusing bytes.Buffer objects.
// Usage pattern:
//   buf := GetBytesBuffer()
//   defer PutBytesBuffer(buf)
//   // use buf...
var BytesBufferPool = sync.Pool{
	New: func() interface{} {
		return new(bytes.Buffer)
	},
}

// GetBytesBuffer returns a bytes.Buffer from the pool
func GetBytesBuffer() *bytes.Buffer {
	return BytesBufferPool.Get().(*bytes.Buffer)
}

// PutBytesBuffer returns a bytes.Buffer to the pool after resetting it
func PutBytesBuffer(buf *bytes.Buffer) {
	if buf == nil {
		return
	}
	buf.Reset()
	BytesBufferPool.Put(buf)
}

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
