package handler

import (
	"strings"
	"sync"

	"github.com/jumpserver/koko/pkg/model"
)

type DatabasePaginator interface {
	Paginator
	RetrievePageData(pageIndex int) []model.Database
	SearchAsset(key string) []model.Database
	SearchAgain(key string) []model.Database
	Name() string
	SearchKeys() []string
}

func NewLocalDatabasePaginator(data []model.Database, pageSize int) DatabasePaginator {
	p := localDatabasePaginator{
		allData:       data,
		currentData:   data,
		currentOffset: 0,
		currentPage:   1,
		search:        make([]string, 0, 4),
		lock:          new(sync.RWMutex),
	}
	p.SetPageSize(pageSize)
	return &p
}

type localDatabasePaginator struct {
	allData []model.Database

	currentData []model.Database

	currentPage int

	pageSize  int
	totalPage int

	currentOffset int

	search []string
	lock   *sync.RWMutex

	currentResult []model.Database
}

func (l *localDatabasePaginator) Name() string {
	return "local"
}

func (l *localDatabasePaginator) SearchKeys() []string {
	return l.search
}

func (l *localDatabasePaginator) HasPrev() bool {
	l.lock.RLock()
	defer l.lock.RUnlock()
	return l.currentPage > 1
}

func (l *localDatabasePaginator) HasNext() bool {
	l.lock.RLock()
	defer l.lock.RUnlock()
	return l.currentPage < l.totalPage
}

func (l *localDatabasePaginator) CurrentPage() int {
	l.lock.RLock()
	defer l.lock.RUnlock()
	return l.currentPage
}

func (l *localDatabasePaginator) TotalCount() int {
	l.lock.RLock()
	defer l.lock.RUnlock()
	return len(l.currentData)
}

func (l *localDatabasePaginator) TotalPage() int {
	l.lock.RLock()
	defer l.lock.RUnlock()
	return l.totalPage
}

func (l *localDatabasePaginator) PageSize() int {
	l.lock.RLock()
	defer l.lock.RUnlock()
	return l.pageSize
}

func (l *localDatabasePaginator) SetPageSize(size int) {
	if size <= 0 {
		size = len(l.currentData)
	}
	l.lock.Lock()
	defer l.lock.Unlock()

	if l.pageSize == size {
		return
	}
	l.pageSize = size
}

func (l *localDatabasePaginator) RetrievePageData(pageIndex int) []model.Database {
	l.lock.Lock()
	defer l.lock.Unlock()
	return l.retrievePageData(pageIndex)
}

func (l *localDatabasePaginator) SearchAsset(key string) []model.Database {
	l.lock.Lock()
	defer l.lock.Unlock()
	l.search = l.search[:0]
	l.search = append(l.search, key)
	l.currentData = searchFromLocalDBs(l.allData, key)
	l.currentPage = 1
	l.currentOffset = 0
	return l.retrievePageData(1)
}

func (l *localDatabasePaginator) SearchAgain(key string) []model.Database {
	l.lock.Lock()
	defer l.lock.Unlock()
	l.currentData = searchFromLocalDBs(l.currentData, key)
	l.search = append(l.search, key)
	l.currentPage = 1
	l.currentOffset = 0
	return l.retrievePageData(1)
}

func (l *localDatabasePaginator) retrievePageData(pageIndex int) []model.Database {
	offsetPage := pageIndex - l.currentPage
	totalOffset := offsetPage * l.pageSize
	l.currentOffset += totalOffset

	switch {
	case l.currentOffset <= 0:
		l.currentOffset = 0
	case l.currentOffset >= len(l.currentData):
		l.currentOffset = len(l.currentData)
	case l.pageSize >= len(l.currentData):
		l.currentOffset = 0
	}

	end := l.currentOffset + l.pageSize
	if end >= len(l.currentData) {
		end = len(l.currentData)
	}
	l.currentResult = l.currentData[l.currentOffset:end]
	l.updatePageInfo()
	return l.currentResult
}

func (l *localDatabasePaginator) updatePageInfo() {
	pageSize := l.pageSize
	totalCount := len(l.currentData)

	switch totalCount % pageSize {
	case 0:
		l.totalPage = totalCount / pageSize
	default:
		l.totalPage = (totalCount / pageSize) + 1
	}
	offset := l.currentOffset + len(l.currentResult)
	switch offset % pageSize {
	case 0:
		l.currentPage = offset / pageSize
	default:
		l.currentPage = (offset / pageSize) + 1
	}
}

func searchFromLocalDBs(dbs []model.Database, key string) []model.Database {
	displayDBs := make([]model.Database, 0, len(dbs))
	key = strings.ToLower(key)
	for _, db := range dbs {
		contents := []string{strings.ToLower(db.Name), strings.ToLower(db.DBName),
			strings.ToLower(db.Host), strings.ToLower(db.Comment)}
		if isSubstring(contents, key) {
			displayDBs = append(displayDBs, db)
		}
	}
	return displayDBs
}
