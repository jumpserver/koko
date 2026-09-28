//go:build linux

package proxy

import (
	"os"

	"github.com/jumpserver/koko/pkg/logger"
	"golang.org/x/sys/unix"
)

// Keep the page cache from tracking the full size of a long-running replay.
const replayCacheWindow = 16 << 20

type replayCacheWriter struct {
	file         *os.File
	sessionID    string
	written      int64
	lastAttempt  int64
	released     int64
	releaseEvery int64
}

func newReplayWriter(file *os.File, sessionID string) *replayCacheWriter {
	return &replayCacheWriter{
		file:         file,
		sessionID:    sessionID,
		releaseEvery: replayCacheWindow,
	}
}

func (w *replayCacheWriter) Write(p []byte) (int, error) {
	n, err := w.file.Write(p)
	w.written += int64(n)
	if err != nil || w.written-w.lastAttempt < w.releaseEvery {
		return n, err
	}
	w.lastAttempt = w.written

	fd := int(w.file.Fd())
	// Dirty pages cannot be discarded; sync before advising on completed pages.
	if err := unix.Fdatasync(fd); err != nil {
		logger.Warnf("Session %s: sync replay before cache release failed: %s", w.sessionID, err)
		return n, nil
	}

	pageSize := int64(os.Getpagesize())
	// FADV_DONTNEED ignores partial pages, so carry the last page forward.
	end := w.written - w.written%pageSize
	if end > w.released {
		if err := unix.Fadvise(fd, w.released, end-w.released, unix.FADV_DONTNEED); err != nil {
			logger.Warnf("Session %s: release replay cache failed: %s", w.sessionID, err)
		} else {
			w.released = end
		}
	}
	return n, nil
}
