//go:build linux || darwin || freebsd || openbsd || netbsd

package engine

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// Each live server holds its own OS lock. A new process can remove abandoned
// exports immediately after a crash without deleting another live server's
// downloads. A short root lock makes directory creation and orphan discovery
// atomic across simultaneous startup processes.
func openExportStore(stateDir string) (string, *os.File, error) {
	if stateDir == "" {
		stateDir = "data/go"
	}
	root := filepath.Join(stateDir, "exports")
	if err := os.MkdirAll(root, 0700); err != nil {
		return "", nil, err
	}
	if err := os.Chmod(root, 0700); err != nil {
		return "", nil, err
	}
	maintenance, err := os.OpenFile(filepath.Join(root, ".maintenance.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return "", nil, err
	}
	defer maintenance.Close() // Closing this descriptor releases the maintenance lock.
	if err = syscall.Flock(int(maintenance.Fd()), syscall.LOCK_EX); err != nil {
		return "", nil, err
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return "", nil, err
	}
	for _, entry := range entries {
		path := filepath.Join(root, entry.Name())
		if strings.HasPrefix(entry.Name(), ".export-") && !entry.IsDir() {
			info, e := entry.Info()
			if e == nil && time.Since(info.ModTime()) > 30*time.Minute {
				_ = os.Remove(path)
			}
			continue
		}
		if !entry.IsDir() || !strings.HasPrefix(entry.Name(), "instance-") {
			continue
		}
		lock, e := os.OpenFile(filepath.Join(path, ".lock"), os.O_RDWR, 0600)
		if e != nil {
			continue
		}
		if syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB) == nil {
			_ = os.RemoveAll(path)
			_ = syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
		}
		_ = lock.Close()
	}
	dir, err := os.MkdirTemp(root, "instance-")
	if err != nil {
		return "", nil, err
	}
	lock, err := os.OpenFile(filepath.Join(dir, ".lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err == nil {
		err = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	}
	if err != nil {
		if lock != nil {
			lock.Close()
		}
		os.RemoveAll(dir)
		return "", nil, err
	}
	return dir, lock, nil
}
func closeExportStore(dir string, lock *os.File) {
	if lock == nil {
		return
	}
	_ = os.RemoveAll(dir)
	_ = syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	_ = lock.Close()
}
