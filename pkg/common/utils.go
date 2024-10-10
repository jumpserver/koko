package common

import (
	"compress/gzip"
	"fmt"
	"io"
	"net/netip"
	"os"
	"sync"
	"time"
	"unsafe"
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

// BytesToString converts byte slice to string without a memory allocation.
func BytesToString(b []byte) string {
	return *(*string)(unsafe.Pointer(&b))
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
	chunkSize := int64(64 * 1024)
	maxConcurrent := 200

	var wg sync.WaitGroup
	chunkCount := int(fileSize / chunkSize)
	if fileSize%chunkSize != 0 {
		chunkCount++
	}

	errChan := make(chan error, chunkCount)
	sem := make(chan struct{}, maxConcurrent)
	for i := 0; i < chunkCount; i++ {
		wg.Add(1)
		sem <- struct{}{}

		go func(chunkIndex int) {
			defer wg.Done()
			defer func() { <-sem }()

			start := int64(chunkIndex) * chunkSize
			end := start + chunkSize
			if end > fileSize {
				end = fileSize
			}

			buf := make([]byte, end-start)
			_, err := readerAt.ReadAt(buf, start)
			if err != nil && err != io.EOF {
				errChan <- fmt.Errorf("failed to read chunk %d: %v", chunkIndex, err)
				return
			}

			_, err = fd.WriteAt(buf, offset+start)
			if err != nil {
				errChan <- fmt.Errorf("failed to write chunk %d: %v", chunkIndex, err)
				return
			}

		}(i)
	}

	wg.Wait()
	close(errChan)

	for err := range errChan {
		if err != nil {
			return err
		}
	}

	return nil
}
