package handler

import (
	"sync"

	"github.com/jumpserver/koko/pkg/model"
	"github.com/jumpserver/koko/pkg/service"
)

type Paginator interface {
	HasPrev() bool
	HasNext() bool
	CurrentPage() int
	TotalCount() int
	TotalPage() int
	PageSize() int
	SetPageSize(size int)
}

type AssetPaginator interface {
	Paginator
	RetrievePageData(pageIndex int) model.AssetList
	SearchAsset(key string) model.AssetList
}

func NewRemoteAssetPaginator(user model.User, pageSize int) AssetPaginator {
	p := remoteAssetsPaginator{
		user:          user,
		pageSize:      pageSize,
		currentOffset: 0,
		currentPage:   1,
		search:        [1]string{},
		lock:          new(sync.RWMutex),
	}
	return &p
}

func NewLocalAssetPaginator(data model.AssetList, pageSize int) AssetPaginator {

	p := localAssetsPaginator{
		allData:       data,
		currentData:   data,
		pageSize:      pageSize,
		currentOffset: 0,
		currentPage:   1,
		search:        [1]string{},
		lock:          new(sync.RWMutex),
	}
	return &p
}

func NewNodeAssetPaginator(user model.User, node model.Node, pageSize int) AssetPaginator {
	p := nodeAssetsPaginator{
		user:          user,
		node:          node,
		pageSize:      pageSize,
		currentOffset: 0,
		currentPage:   1,
		lock:          new(sync.RWMutex),
	}
	return &p
}

type remoteAssetsPaginator struct {
	user     model.User
	pageSize int

	lock          *sync.RWMutex
	currentOffset int
	search        [1]string

	currentData model.AssetList
	totalPage   int
	currentPage int
	totalCount  int

	preUrl  string
	nextUrl string
}

func (r *remoteAssetsPaginator) HasPrev() bool {
	r.lock.RLock()
	defer r.lock.RUnlock()
	return r.preUrl != ""
}

func (r *remoteAssetsPaginator) HasNext() bool {
	r.lock.RLock()
	defer r.lock.RUnlock()
	return r.nextUrl != ""
}

func (r *remoteAssetsPaginator) CurrentPage() int {
	r.lock.RLock()
	defer r.lock.RUnlock()
	return r.currentPage
}

func (r *remoteAssetsPaginator) TotalCount() int {
	r.lock.RLock()
	defer r.lock.RUnlock()
	return r.totalCount
}

func (r *remoteAssetsPaginator) TotalPage() int {
	r.lock.RLock()
	defer r.lock.RUnlock()
	return r.totalPage
}

func (r *remoteAssetsPaginator) PageSize() int {
	r.lock.RLock()
	defer r.lock.RUnlock()
	if r.pageSize == 0 {
		// size 0, 则获取全部资产
		return r.totalCount
	}
	return r.pageSize
}

func (r *remoteAssetsPaginator) SetPageSize(size int) {
	r.lock.Lock()
	defer r.lock.Unlock()
	if size < 0 {
		// size 0, 则获取全部资产
		size = 0
	}
	if r.pageSize == size {
		return
	}
	r.pageSize = size
}

func (r *remoteAssetsPaginator) RetrievePageData(pageIndex int) model.AssetList {
	r.lock.Lock()
	defer r.lock.Unlock()
	return r.retrievePageDta(pageIndex, r.search[0])
}

func (r *remoteAssetsPaginator) RetrieveCurrentData() model.AssetList {
	r.lock.RLock()
	defer r.lock.RUnlock()
	return r.currentData
}

func (r *remoteAssetsPaginator) SearchAsset(key string) model.AssetList {
	r.lock.Lock()
	defer r.lock.Unlock()
	r.search[0] = key
	r.currentPage = 1
	r.currentOffset = 0
	return r.retrievePageDta(1, r.search[0])
}

func (r *remoteAssetsPaginator) retrievePageDta(pageIndex int, search string) model.AssetList {
	offsetPage := pageIndex - r.currentPage
	totalOffset := offsetPage * r.pageSize

	r.currentOffset += totalOffset

	if r.pageSize == 0 || r.currentOffset < 0 || r.pageSize >= r.totalCount {
		r.currentOffset = 0
	}
	res := service.GetUserAssets(r.user.ID, search, r.pageSize, r.currentOffset)

	// update page info data,
	r.totalCount = res.Total
	r.nextUrl = res.NextURL
	r.preUrl = res.PreviousURL
	r.currentData = res.Data
	r.updatePageInfo()
	return res.Data
}

func (r *remoteAssetsPaginator) updatePageInfo() {
	switch r.pageSize {
	case 0:
		r.totalPage = 1
		r.currentPage = 1
	default:
		pageSize := r.pageSize
		totalCount := r.totalCount

		switch totalCount % pageSize {
		case 0:
			r.totalPage = totalCount / pageSize
		default:
			r.totalPage = (totalCount / pageSize) + 1
		}
		currentOffset := r.currentOffset + len(r.currentData)

		switch currentOffset % pageSize {
		case 0:
			r.currentPage = currentOffset / pageSize
		default:
			r.currentPage = (currentOffset / pageSize) + 1
		}
	}
}

type localAssetsPaginator struct {
	allData model.AssetList

	currentData model.AssetList

	currentPage int

	pageSize  int
	totalPage int

	currentOffset int

	search [1]string
	lock   *sync.RWMutex

	currentResult model.AssetList
}

func (l *localAssetsPaginator) HasPrev() bool {
	l.lock.RLock()
	defer l.lock.RUnlock()
	return l.currentPage > 1
}

func (l *localAssetsPaginator) HasNext() bool {
	l.lock.RLock()
	defer l.lock.RUnlock()
	return l.currentPage < l.totalPage
}

func (l *localAssetsPaginator) CurrentPage() int {
	l.lock.RLock()
	defer l.lock.RUnlock()
	return l.currentPage
}

func (l *localAssetsPaginator) TotalCount() int {
	l.lock.RLock()
	defer l.lock.RUnlock()
	return len(l.currentData)
}

func (l *localAssetsPaginator) TotalPage() int {
	l.lock.RLock()
	defer l.lock.RUnlock()
	return l.totalPage
}

func (l *localAssetsPaginator) PageSize() int {
	l.lock.RLock()
	defer l.lock.RUnlock()
	return l.pageSize
}

func (l *localAssetsPaginator) SetPageSize(size int) {
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

func (l *localAssetsPaginator) RetrievePageData(pageIndex int) model.AssetList {
	l.lock.Lock()
	defer l.lock.Unlock()
	return l.retrievePageData(pageIndex)
}

func (l *localAssetsPaginator) SearchAsset(key string) model.AssetList {
	l.lock.Lock()
	defer l.lock.Unlock()
	if key != "" {
		l.currentData = searchFromLocalAssets(l.allData, key)
	} else {
		l.currentData = l.allData
	}
	l.currentPage = 1
	l.currentOffset = 0
	return l.retrievePageData(1)
}

func (l *localAssetsPaginator) retrievePageData(pageIndex int) model.AssetList {
	offsetPage := pageIndex - l.currentPage
	totalOffset := offsetPage * l.pageSize
	l.currentOffset += totalOffset

	if l.currentOffset <= 0 {
		l.currentOffset = 0
	} else if l.currentOffset >= len(l.currentData) {
		l.currentOffset = len(l.currentData)
	}

	end := l.currentOffset + l.pageSize
	if end >= len(l.currentData) {
		end = len(l.currentData)
	}
	l.currentResult = l.currentData[l.currentOffset:end]
	l.updatePageInfo()
	return l.currentResult
}

func (l *localAssetsPaginator) updatePageInfo() {
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

type nodeAssetsPaginator struct {
	user model.User
	node model.Node

	currentPage int
	pageSize    int
	totalPage   int
	totalCount  int

	lock *sync.RWMutex

	currentData model.AssetList

	preUrl        string
	nextUrl       string
	currentOffset int
}

func (n *nodeAssetsPaginator) HasPrev() bool {
	n.lock.RLock()
	defer n.lock.RUnlock()
	return n.preUrl != ""
}

func (n *nodeAssetsPaginator) HasNext() bool {
	n.lock.RLock()
	defer n.lock.RUnlock()
	return n.nextUrl != ""
}

func (n *nodeAssetsPaginator) CurrentPage() int {
	n.lock.RLock()
	defer n.lock.RUnlock()
	return n.currentPage
}

func (n *nodeAssetsPaginator) TotalCount() int {
	n.lock.RLock()
	defer n.lock.RUnlock()
	return n.totalCount
}

func (n *nodeAssetsPaginator) TotalPage() int {
	n.lock.RLock()
	defer n.lock.RUnlock()
	return n.totalPage
}

func (n *nodeAssetsPaginator) PageSize() int {
	n.lock.RLock()
	defer n.lock.RUnlock()
	if n.pageSize == 0 {
		// size 0, 则获取全部资产
		return n.totalCount
	}
	return n.pageSize
}

func (n *nodeAssetsPaginator) SetPageSize(size int) {
	n.lock.Lock()
	defer n.lock.Unlock()
	if size < 0 {
		// size 0, 则获取全部资产
		size = 0
	}
	if n.pageSize == size {
		return
	}
	n.pageSize = size
}

func (n *nodeAssetsPaginator) RetrievePageData(pageIndex int) model.AssetList {
	n.lock.Lock()
	defer n.lock.Unlock()
	return n.retrievePageData(pageIndex)
}

func (n *nodeAssetsPaginator) SearchAsset(key string) model.AssetList {
	n.lock.Lock()
	defer n.lock.Unlock()
	n.currentPage = 1
	n.currentOffset = 0
	return n.RetrievePageData(1)
}

func (n *nodeAssetsPaginator) retrievePageData(pageIndex int) model.AssetList {
	offsetPage := pageIndex - n.currentPage
	totalOffset := offsetPage * n.pageSize

	n.currentOffset += totalOffset

	if n.pageSize == 0 || n.currentOffset < 0 || n.pageSize >= n.totalCount {
		n.currentOffset = 0
	}
	res := service.GetUserNodePaginationAssets(n.user.ID, n.node.ID, n.pageSize, n.currentOffset)
	n.totalCount = res.Total
	n.nextUrl = res.NextURL
	n.preUrl = res.PreviousURL
	n.currentData = res.Data
	n.updatePageInfo()
	return res.Data
}

func (n *nodeAssetsPaginator) updatePageInfo() {
	switch n.pageSize {
	case 0:
		n.totalPage = 1
		n.currentPage = 1
	default:
		pageSize := n.pageSize
		totalCount := n.totalCount

		switch totalCount % pageSize {
		case 0:
			n.totalPage = totalCount / pageSize
		default:
			n.totalPage = (totalCount / pageSize) + 1
		}
		currentOffset := n.currentOffset + len(n.currentData)
		switch currentOffset % pageSize {
		case 0:
			n.currentPage = currentOffset / pageSize
		default:
			n.currentPage = (currentOffset / pageSize) + 1
		}
	}
}
