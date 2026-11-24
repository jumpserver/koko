//go:build !linux && !darwin && !freebsd && !openbsd && !netbsd

package engine

import (
	"errors"
	"os"
)

func openExportStore(string) (string, *os.File, error) {
	return "", nil, errors.New("secure export storage requires process file locks on this platform")
}
func closeExportStore(string, *os.File) {}
