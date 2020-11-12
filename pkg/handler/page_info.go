package handler

const PAGESIZEALL = 0

type pageInfo struct {
	pageSize   int
	totalCount int

	currentOffset int

	totalPage   int
	currentPage int
}

func (p *pageInfo) updatePageInfo(pageSiz, totalCount, offset int) {
	p.pageSize = pageSiz
	p.totalCount = totalCount
	p.currentOffset = offset
	p.update()
}

func (p *pageInfo) update() {
	// 根据 pageSize和total值 更新  totalPage currentPage
	if p.pageSize <= 0 {
		p.totalPage = 1
		p.currentPage = 1
		return
	}
	pageSize := p.pageSize
	totalCount := p.totalCount

	switch totalCount % pageSize {
	case 0:
		p.totalPage = totalCount / pageSize
	default:
		p.totalPage = (totalCount / pageSize) + 1
	}
	switch p.currentOffset % pageSize {
	case 0:
		p.currentPage = p.currentOffset / pageSize
	default:
		p.currentPage = (p.currentOffset / pageSize) + 1
	}
}

func (p *pageInfo) TotalCount() int {
	return p.totalCount
}

func (p *pageInfo) TotalPage() int {
	return p.totalPage
}
func (p *pageInfo) PageSize() int {
	return p.pageSize
}
func (p *pageInfo) CurrentPage() int {
	return p.currentPage
}

func (p *pageInfo) CurrentOffSet() int {
	return p.currentOffset
}
