package store

import "golang.org/x/sys/windows"

func availableDiskBytes(path string) (int64, error) {
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, err
	}
	var free uint64
	if err = windows.GetDiskFreeSpaceEx(name, &free, nil, nil); err != nil {
		return 0, err
	}
	return int64(free), nil
}

// Windows does not support fsync on a directory handle; the snapshot file has
// already been flushed before os.Rename replaces the journal.
func syncJournalDirectory(string) error { return nil }
