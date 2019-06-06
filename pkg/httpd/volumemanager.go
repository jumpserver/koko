package httpd

import (
	"sync"
)

var userVolumes = make(map[string]*UserVolume)
var volumeLock = new(sync.RWMutex)

func addUserVolume(sid string, v *UserVolume) {
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

func GetUserVolume(sid string) (*UserVolume, bool) {
	volumeLock.RLock()
	defer volumeLock.RUnlock()
	v, ok := userVolumes[sid]
	return v, ok
}
