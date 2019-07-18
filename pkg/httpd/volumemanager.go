package httpd

import (
	"sync"

	"github.com/LeeEirc/elfinder"
)

type VolumeCloser interface {
	elfinder.Volume
	Close()
}

var userVolumes = make(map[string]VolumeCloser)
var volumeLock = new(sync.RWMutex)

func addUserVolume(sid string, v VolumeCloser) {
	volumeLock.Lock()
	defer volumeLock.Unlock()
	userVolumes[sid] = v
}

func removeUserVolume(sid string) {
	volumeLock.RLock()
	v, ok := userVolumes[sid]
	volumeLock.RUnlock()
	if !ok {
		return
	}
	v.Close()
	volumeLock.Lock()
	delete(userVolumes, sid)
	volumeLock.Unlock()

}

func GetUserVolume(sid string) (VolumeCloser, bool) {
	volumeLock.RLock()
	defer volumeLock.RUnlock()
	v, ok := userVolumes[sid]
	return v, ok
}
