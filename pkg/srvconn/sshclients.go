package srvconn

import (
	"time"

	"github.com/jumpserver/koko/pkg/logger"
)

type UserSSHClient struct {
	ID   string // 这个 user ssh client key 参考 MakeReuseSSHClientKey
	data map[*SSHClient]int64
	name string
}

func (u *UserSSHClient) AddClient(client *SSHClient) {
	u.data[client] = time.Now().UnixNano()
}

func (u *UserSSHClient) GetClient() *SSHClient {
	var selectClient *SSHClient
	var refCount int32
	// 取引用最少的 SSHClient
	for clientItem := range u.data {
		if refCount <= clientItem.RefCount() {
			refCount = clientItem.RefCount()
			selectClient = clientItem
		}
	}
	return selectClient
}

func (u *UserSSHClient) recycleClients() {
	needRemovedClients := make([]*SSHClient, 0, len(u.data))
	for client := range u.data {
		if client.RefCount() <= 0 && client.selfRef() <= 0 {
			needRemovedClients = append(needRemovedClients, client)
			_ = client.Close()
		}
	}
	if len(needRemovedClients) > 0 {
		for i := range needRemovedClients {
			delete(u.data, needRemovedClients[i])
		}
		logger.Infof("Remove %d clients (%s) remain %d",
			len(needRemovedClients), u.name, len(u.data))
	}
}

func (u *UserSSHClient) Count() int {
	return len(u.data)
}

func newSSHManager() *SSHManager {
	m := SSHManager{
		storeChan:  make(chan *storeClient),
		reqChan:    make(chan string),
		resultChan: make(chan *SSHClient),

		releaseChan: make(chan *storeClient),
	}
	go m.run()
	return &m
}

type SSHManager struct {
	storeChan  chan *storeClient
	reqChan    chan string // reqId
	resultChan chan *SSHClient

	releaseChan chan *storeClient
}

func (s *SSHManager) run() {
	tick := time.NewTicker(time.Minute)
	defer tick.Stop()
	data := make(map[string]*UserSSHClient)
	latestVisited := time.Now()
	for {
		select {
		case now := <-tick.C:
			/*
				1. 1 分钟无访问则 让所有的 UserSSHClient recycleClients
				2. 并清理 count==0 的 UserSSHClient
			*/
			if now.After(latestVisited.Add(time.Minute)) {
				needRemovedClients := make([]string, 0, len(data))
				for key, userClient := range data {
					userClient.recycleClients()
					if userClient.Count() == 0 {
						needRemovedClients = append(needRemovedClients, key)
					}
				}
				if len(needRemovedClients) > 0 {
					for i := range needRemovedClients {
						delete(data, needRemovedClients[i])
					}
					logger.Infof("Remove %d cache ssh clients remain %d",
						len(needRemovedClients), len(data))
				}
			}
			continue
		case reqKey := <-s.reqChan:
			var foundClient *SSHClient
			if userClient, ok := data[reqKey]; ok {
				foundClient = userClient.GetClient()
				logger.Infof("Found client(%s) and remain %d",
					foundClient, userClient.Count())
			}
			s.resultChan <- foundClient

		case reqClient := <-s.storeChan:
			reqClient.SSHClient.increaseSelfRef()
			userClient, ok := data[reqClient.key]
			if !ok {
				userClient = &UserSSHClient{
					ID:   reqClient.key,
					name: reqClient.SSHClient.String(),
					data: make(map[*SSHClient]int64),
				}
				data[reqClient.key] = userClient
			}
			userClient.AddClient(reqClient.SSHClient)
			logger.Infof("Store new client(%s) remain %d", reqClient.String(), userClient.Count())
		case reqClient := <-s.releaseChan:
			// 收到释放请求，及时释放对应的 SSHClient
			reqClient.decreaseSelfRef()
			if userClient, ok := data[reqClient.key]; ok {
				userClient.recycleClients()
			} else {
				_ = reqClient.Close()
				logger.Infof("SSH client(%s) not found in user ssh cache and close", reqClient.String())
			}
		}

		latestVisited = time.Now()
	}
}

func (s *SSHManager) getClientFromCache(key string) (*SSHClient, bool) {
	s.reqChan <- key
	client := <-s.resultChan
	return client, client != nil
}

func (s *SSHManager) AddClientCache(key string, client *SSHClient) {
	s.storeChan <- &storeClient{
		key:       key,
		SSHClient: client,
	}
}

func (s *SSHManager) ReleaseClientCacheKey(key string, client *SSHClient) {
	s.releaseChan <- &storeClient{
		key:       key,
		SSHClient: client,
	}
}

type storeClient struct {
	key string
	*SSHClient
}
