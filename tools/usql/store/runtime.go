package store

import (
	"sync"
)

var s *Store

func init() {
	s = NewStore()
}

func GetGlobalStore() *Store {
	return s
}

// Store 是一个线程安全的全局 key-value 存储类
type Store struct {
	data map[string]interface{}
	lock sync.RWMutex
}

// NewStore 初始化一个新的 Store
func NewStore() *Store {
	return &Store{
		data: make(map[string]interface{}),
	}
}

// Set 设置 key 对应的值
func (s *Store) Set(key string, value interface{}) {
	s.lock.Lock()
	defer s.lock.Unlock()
	s.data[key] = value
}

// Get 获取 key 对应的值，如果不存在返回 nil 和 false
func (s *Store) Get(key string) (interface{}, bool) {
	s.lock.RLock()
	defer s.lock.RUnlock()
	val, ok := s.data[key]
	return val, ok
}

// Delete 删除一个 key
func (s *Store) Delete(key string) {
	s.lock.Lock()
	defer s.lock.Unlock()
	delete(s.data, key)
}

// Keys 返回所有的 key 列表
func (s *Store) Keys() []string {
	s.lock.RLock()
	defer s.lock.RUnlock()
	keys := make([]string, 0, len(s.data))
	for k := range s.data {
		keys = append(keys, k)
	}
	return keys
}
