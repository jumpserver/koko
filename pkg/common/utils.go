package common

import (
	"compress/gzip"
	"fmt"
	"io"
	"net/netip"
	"os"
	"sync"
	"time"
)

func FileExists(name string) bool {
	if _, err := os.Stat(name); err != nil {
		if os.IsNotExist(err) {
			return false
		}
	}
	return true
}

func EnsureDirExist(name string) error {
	if !FileExists(name) {
		return os.MkdirAll(name, os.ModePerm)
	}
	return nil
}

func GzipCompressFile(srcPath, dstPath string) error {
	sf, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer sf.Close()
	sfInfo, err := sf.Stat()
	if err != nil {
		return err
	}
	df, err := os.Create(dstPath)
	if err != nil {
		return err
	}
	defer df.Close()
	writer := gzip.NewWriter(df)
	writer.Name = sfInfo.Name()
	writer.ModTime = time.Now().UTC()
	_, err = io.Copy(writer, sf)
	if err != nil {
		return err
	}
	if err = writer.Close(); err != nil {
		return err
	}
	return nil
}

func Sum(i []int) int {
	sum := 0
	for _, v := range i {
		sum += v
	}
	return sum
}

func Abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

func CurrentUTCTime() string {
	return time.Now().UTC().Format("2006-01-02 15:04:05 +0000")
}

func CompareString(a, b string) bool {
	return a < b
}

func CompareIP(ipA, ipB string) bool {
	addrA, err := netip.ParseAddr(ipA)
	if err != nil {
		return false
	}
	addrB, err := netip.ParseAddr(ipB)
	if err != nil {
		return false
	}
	return addrA.Less(addrB)
}

func ChunkedFileTransfer(fd io.WriterAt, readerAt io.ReaderAt, offset, fileSize int64) error {
	if fileSize < 0 {
		return fmt.Errorf("invalid file size")
	}
	const chunkSize int64 = 64 * 1024
	const maxConcurrent = 200
	chunkCount := int(fileSize / chunkSize)
	if fileSize%chunkSize != 0 {
		chunkCount++
	}
	if chunkCount == 0 {
		return nil
	}
	workers := min(chunkCount, maxConcurrent)
	jobs := make(chan int)
	done := make(chan struct{})
	var wg sync.WaitGroup
	var once sync.Once
	var firstErr error
	fail := func(err error) {
		once.Do(func() {
			firstErr = err
			close(done)
		})
	}

	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-done:
					return
				case chunkIndex, ok := <-jobs:
					if !ok {
						return
					}
					start := int64(chunkIndex) * chunkSize
					end := min(start+chunkSize, fileSize)
					buf := make([]byte, end-start)
					n, err := readerAt.ReadAt(buf, start)
					if err != nil && err != io.EOF {
						fail(fmt.Errorf("failed to read chunk %d: %w", chunkIndex, err))
						return
					}
					if n != len(buf) {
						fail(fmt.Errorf("failed to read chunk %d: %w", chunkIndex, io.ErrUnexpectedEOF))
						return
					}
					n, err = fd.WriteAt(buf, offset+start)
					if err != nil {
						fail(fmt.Errorf("failed to write chunk %d: %w", chunkIndex, err))
						return
					}
					if n != len(buf) {
						fail(fmt.Errorf("failed to write chunk %d: %w", chunkIndex, io.ErrShortWrite))
						return
					}
				}
			}
		}()
	}

	for chunkIndex := 0; chunkIndex < chunkCount; chunkIndex++ {
		select {
		case <-done:
			close(jobs)
			wg.Wait()
			return firstErr
		case jobs <- chunkIndex:
		}
	}
	close(jobs)
	wg.Wait()
	return firstErr
}
