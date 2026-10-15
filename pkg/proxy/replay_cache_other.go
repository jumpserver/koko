//go:build !linux

package proxy

import "os"

func newReplayWriter(file *os.File, _ string) *os.File {
	return file
}
