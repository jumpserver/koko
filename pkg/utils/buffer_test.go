package utils

import (
	"bytes"
	"sync"
	"testing"
)

func TestBufferPool(t *testing.T) {
	pool := NewBufferPool(1024)

	// Get a buffer
	buf := pool.Get()
	if buf == nil {
		t.Fatal("Expected non-nil buffer")
	}
	if len(*buf) != 1024 {
		t.Fatalf("Expected buffer length 1024, got %d", len(*buf))
	}

	// Use the buffer
	copy(*buf, []byte("test data"))

	// Return buffer to pool
	pool.Put(buf)

	// Get another buffer - should reuse the pooled one
	buf2 := pool.Get()
	if buf2 == nil {
		t.Fatal("Expected non-nil buffer")
	}
	if len(*buf2) != 1024 {
		t.Fatalf("Expected buffer length 1024, got %d", len(*buf2))
	}
	pool.Put(buf2)
}

func TestBufferPoolConcurrent(t *testing.T) {
	pool := NewBufferPool(512)
	var wg sync.WaitGroup

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			buf := pool.Get()
			if buf == nil {
				t.Error("Expected non-nil buffer")
				return
			}
			// Simulate some work
			copy(*buf, []byte("concurrent test"))
			pool.Put(buf)
		}()
	}
	wg.Wait()
}

func TestGlobalBufferPools(t *testing.T) {
	// Test SmallBufferPool
	smallBuf := SmallBufferPool.Get()
	if smallBuf == nil || len(*smallBuf) != SmallBufferSize {
		t.Errorf("SmallBufferPool returned invalid buffer")
	}
	SmallBufferPool.Put(smallBuf)

	// Test MediumBufferPool
	mediumBuf := MediumBufferPool.Get()
	if mediumBuf == nil || len(*mediumBuf) != MediumBufferSize {
		t.Errorf("MediumBufferPool returned invalid buffer")
	}
	MediumBufferPool.Put(mediumBuf)

	// Test LargeBufferPool
	largeBuf := LargeBufferPool.Get()
	if largeBuf == nil || len(*largeBuf) != LargeBufferSize {
		t.Errorf("LargeBufferPool returned invalid buffer")
	}
	LargeBufferPool.Put(largeBuf)
}

func TestBytesBufferPool(t *testing.T) {
	buf := GetBytesBuffer()
	if buf == nil {
		t.Fatal("Expected non-nil bytes.Buffer")
	}

	buf.WriteString("test data")
	if buf.String() != "test data" {
		t.Errorf("Expected 'test data', got '%s'", buf.String())
	}

	PutBytesBuffer(buf)

	// Get another buffer
	buf2 := GetBytesBuffer()
	if buf2 == nil {
		t.Fatal("Expected non-nil bytes.Buffer")
	}
	// Buffer should be reset
	if buf2.Len() != 0 {
		t.Errorf("Expected empty buffer, got length %d", buf2.Len())
	}
	PutBytesBuffer(buf2)
}

func TestSyncBuffer(t *testing.T) {
	buf := NewMaxSizeBuffer(100)

	// Test write within limit
	data := []byte("hello world")
	n, err := buf.Write(data)
	if err != nil {
		t.Fatalf("Write failed: %v", err)
	}
	if n != len(data) {
		t.Errorf("Expected write of %d bytes, got %d", len(data), n)
	}

	if buf.String() != "hello world" {
		t.Errorf("Expected 'hello world', got '%s'", buf.String())
	}

	// Test write exceeding limit
	largeData := make([]byte, 100)
	n, err = buf.Write(largeData)
	if err != nil {
		t.Fatalf("Write failed: %v", err)
	}
	if n != len(largeData) {
		t.Errorf("Expected write of %d bytes, got %d", len(largeData), n)
	}
	// Should not have written as it would exceed max size
	if buf.String() != "hello world" {
		t.Errorf("Buffer should not have changed, got '%s'", buf.String())
	}
}

func BenchmarkBufferPoolGet(b *testing.B) {
	pool := NewBufferPool(1024)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf := pool.Get()
		pool.Put(buf)
	}
}

func BenchmarkRawBufferAlloc(b *testing.B) {
	for i := 0; i < b.N; i++ {
		buf := make([]byte, 1024)
		_ = buf
	}
}

func BenchmarkBytesBufferPoolGet(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf := GetBytesBuffer()
		buf.WriteString("test data for benchmarking")
		PutBytesBuffer(buf)
	}
}

func BenchmarkRawBytesBufferAlloc(b *testing.B) {
	for i := 0; i < b.N; i++ {
		buf := new(bytes.Buffer)
		buf.WriteString("test data for benchmarking")
		_ = buf
	}
}
