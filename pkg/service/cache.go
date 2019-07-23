package service

import (
	"sync"

	"github.com/jumpserver/koko/pkg/model"
)

type assetsCacheContainer struct {
	mapData map[string]model.AssetList
	mapETag map[string]string
	mu      *sync.RWMutex
}

func (c *assetsCacheContainer) Get(key string) (model.AssetList, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	value, ok := c.mapData[key]
	return value, ok
}

func (c *assetsCacheContainer) GetETag(key string) (string, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	value, ok := c.mapETag[key]
	return value, ok
}

func (c *assetsCacheContainer) SetValue(key string, value model.AssetList) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.mapData[key] = value
}

func (c *assetsCacheContainer) SetETag(key string, value string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.mapETag[key] = value
}

type nodesCacheContainer struct {
	mapData map[string]model.NodeList
	mapETag map[string]string
	mu      *sync.RWMutex
}

func (c *nodesCacheContainer) Get(key string) (model.NodeList, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	value, ok := c.mapData[key]
	return value, ok
}

func (c *nodesCacheContainer) GetETag(key string) (string, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	value, ok := c.mapETag[key]
	return value, ok
}

func (c *nodesCacheContainer) SetValue(key string, value model.NodeList) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.mapData[key] = value
}

func (c *nodesCacheContainer) SetETag(key string, value string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.mapETag[key] = value
}
