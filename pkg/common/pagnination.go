package common

import "sync"

func NewPagination(data []interface{}, size int) *Pagination {
	p := &Pagination{
		data: data,
		lock: new(sync.RWMutex),
	}
	p.SetPageSize(size)
	return p
}

type Pagination struct {
	data []interface{}

	currentPage int
	pageSize    int
	totalPage   int
	lock        *sync.RWMutex
}

func (p *Pagination) GetNextPageData() []interface{} {
	if !p.HasNextPage() {
		return []interface{}{}
	}
	p.lock.Lock()
	p.currentPage++
	p.lock.Unlock()
	return p.GetPageData(p.currentPage)
}

func (p *Pagination) GetPrePageData() []interface{} {
	if !p.HasPrePage() {
		return []interface{}{}
	}
	p.lock.Lock()
	p.currentPage--
	p.lock.Unlock()
	return p.GetPageData(p.currentPage)
}

func (p *Pagination) GetPageData(pageIndex int) []interface{} {
	p.lock.RLock()
	defer p.lock.RUnlock()
	var (
		endIndex   int
		startIndex int
	)

	endIndex = p.pageSize * pageIndex
	startIndex = endIndex - p.pageSize
	if endIndex > len(p.data) {
		endIndex = len(p.data)
	}
	return p.data[startIndex:endIndex]
}

func (p *Pagination) CurrentPage() int {
	p.lock.RLock()
	defer p.lock.RUnlock()
	return p.currentPage
}

func (p *Pagination) TotalCount() int {
	p.lock.RLock()
	defer p.lock.RUnlock()
	return len(p.data)
}

func (p *Pagination) TotalPage() int {
	p.lock.RLock()
	defer p.lock.RUnlock()
	return p.totalPage
}

func (p *Pagination) SetPageSize(size int) {

	if size <= 0 {
		panic("Pagination size should be larger than zero")
	}
	p.lock.Lock()
	defer p.lock.Unlock()
	if p.pageSize == size {
		return
	}
	p.pageSize = size
	if len(p.data)%size == 0 {
		p.totalPage = len(p.data) / size
	} else {
		p.totalPage = len(p.data)/size + 1
	}
	p.currentPage = 1

}

func (p *Pagination) GetPageSize() int {
	p.lock.RLock()
	defer p.lock.RUnlock()
	return p.pageSize
}

func (p *Pagination) HasNextPage() bool {
	p.lock.RLock()
	defer p.lock.RUnlock()
	return p.currentPage < p.totalPage
}

func (p *Pagination) HasPrePage() bool {
	p.lock.RLock()
	defer p.lock.RUnlock()
	return p.currentPage > 1
}
