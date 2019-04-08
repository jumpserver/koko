package core

import (
	"context"
	"sync"
)

type room struct {
	sessionID string
	uHome     SessionHome
	pChan     ProxyChannel
}

var Manager = &manager{container: map[string]room{}}

type manager struct {
	container map[string]room
	sync.RWMutex
}

func (m *manager) add(uHome SessionHome, pChan ProxyChannel) {
	m.Lock()
	m.container[uHome.SessionID()] = room{
		sessionID: uHome.SessionID(),
		uHome:     uHome,
		pChan:     pChan,
	}
	m.Unlock()
}

func (m *manager) delete(roomID string) {
	m.Lock()
	delete(m.container, roomID)
	m.Unlock()
}

func (m *manager) search(roomID string) (SessionHome, bool) {
	m.RLock()
	defer m.RUnlock()
	if room, ok := m.container[roomID]; ok {
		return room.uHome, ok
	}
	return nil, false
}

func JoinShareRoom(roomID string, uConn Conn) {
	if userHome, ok := Manager.search(roomID); ok {
		userHome.AddConnection(uConn)
	}
}

func ExitShareRoom(roomID string, uConn Conn) {
	if userHome, ok := Manager.search(roomID); ok {
		userHome.RemoveConnection(uConn)
	}

}

func Switch(ctx context.Context, userHome SessionHome, pChannel ProxyChannel) error {
	Manager.add(userHome, pChannel)
	defer Manager.delete(userHome.SessionID())
	subCtx, cancel := context.WithCancel(ctx)
	var wg sync.WaitGroup
	wg.Add(2)
	go func(ctx context.Context, wg *sync.WaitGroup) {
		defer wg.Done()

		userSendRequestStream := userHome.SendRequestChannel(ctx)
		nodeRequestChan := pChannel.ReceiveRequestChannel(ctx)

		for reqFromUser := range userSendRequestStream {
			nodeRequestChan <- reqFromUser
		}
		log.Info("userSendRequestStream close")
		close(nodeRequestChan)

	}(subCtx, &wg)

	go func(ctx context.Context, wg *sync.WaitGroup) {
		defer wg.Done()
		userReceiveStream := userHome.ReceiveResponseChannel(ctx)
		nodeSendResponseStream := pChannel.SendResponseChannel(ctx)

		for resFromNode := range nodeSendResponseStream {
			userReceiveStream <- resFromNode
		}
		log.Info("nodeSendResponseStream close")
		close(userReceiveStream)
	}(subCtx, &wg)
	err := pChannel.Wait()
	if err != nil {
		log.Info("pChannel err:", err)
	}
	cancel()
	wg.Wait()
	log.Info("switch end")
	return err

}
