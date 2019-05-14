package handler

import (
	"fmt"
	"strconv"
	"strings"
	"sync"

	"github.com/olekukonko/tablewriter"

	"cocogo/pkg/config"
	"cocogo/pkg/model"
	"cocogo/pkg/utils"
)

const (
	IDColumnMinSize       = 4
	HostNameColumnMaxSize = 15
	IPColumnSize          = 16
	CommentColumnMinSize  = 2
)

func NewAssetPagination(term *utils.Terminal, assets []model.Asset) *AssetPagination {
	fields := []string{"ID", "Hostname", "IP", "LoginAs", "Comment"}
	wtable := WrapperTable{
		Fields:     fields,
		DataBulk:   make([][]string, 0),
		ColumnSize: make([]int, len(fields)),
	}
	var interfaceSlice = make([]interface{}, len(assets))
	for i, d := range assets {
		interfaceSlice[i] = d
	}
	page := &Pagination{
		data:        interfaceSlice,
		lock:        new(sync.RWMutex),
		currentPage: 1,
	}

	assetPage := &AssetPagination{term: term,
		tableWriter: &wtable,
		page:        page,
	}
	assetPage.Initial()
	return assetPage

}

type AssetPagination struct {
	term        *utils.Terminal
	tableWriter *WrapperTable
	page        *Pagination
	currentData []model.Asset
}

func (p *AssetPagination) Initial() {
	var (
		pageSize int
	)
	_, height := p.term.GetSize()
	switch config.Conf.AssetListPageSize {
	case "auto":
		pageSize = height - 7
	case "all":
		pageSize = len(p.page.data)
	default:
		if value, err := strconv.Atoi(config.Conf.AssetListPageSize); err == nil {
			pageSize = value
		} else {
			pageSize = height - 7
		}
	}
	if pageSize <= 0 {
		pageSize = 1
	}
	p.page.SetPageSize(pageSize)
	tmpdata := p.page.GetPageData(1)
	p.currentData = make([]model.Asset, len(tmpdata))

	for i, item := range tmpdata {
		p.currentData[i] = item.(model.Asset)
	}

}

func (p *AssetPagination) setPageSize() {
	if config.Conf.AssetListPageSize == "auto" {
		var pageSize int
		_, height := p.term.GetSize()
		remainSize := height - 7
		if remainSize > 0 {
			pageSize = remainSize
		} else {
			pageSize = 1
		}
		if p.page.GetPageSize() != pageSize {
			p.page.SetPageSize(pageSize)
		}
	}
}

func (p *AssetPagination) getColumnMaxSize() {
	var (
		IDSize         int
		HostNameSize   int
		systemUserSize int
		CommentSize    int
	)
	IDSize = IDColumnMinSize
	CommentSize = CommentColumnMinSize

	if len(strconv.Itoa(len(p.currentData))) > IDColumnMinSize {
		IDSize = len(strconv.Itoa(len(p.currentData)))
	}
	p.tableWriter.DataBulk = p.tableWriter.DataBulk[:0]
	for i, item := range p.currentData {
		tmpDat := make([]string, 5)
		var tmpSystemUserArray []string
		result := selectHighestPrioritySystemUsers(item.SystemUsers)
		tmpSystemUserArray = make([]string, len(result))
		for index, sysUser := range result {
			tmpSystemUserArray[index] = sysUser.Name
		}
		tmpSystemUserStr := fmt.Sprintf("[%s]", strings.Join(tmpSystemUserArray, ","))
		if len(tmpSystemUserStr) > systemUserSize {
			systemUserSize = len(tmpSystemUserStr)
		}

		if len(item.Hostname) >= HostNameColumnMaxSize {
			HostNameSize = HostNameColumnMaxSize
			tmpDat[1] = item.Hostname[:HostNameColumnMaxSize]
		} else if len(item.Hostname) < HostNameColumnMaxSize && len(item.Hostname) > HostNameSize {
			HostNameSize = len(item.Hostname)
			tmpDat[1] = item.Hostname
		} else {
			tmpDat[1] = item.Hostname
		}

		if len(item.Comment) > CommentSize {
			CommentSize = len(item.Comment)
		}
		tmpDat[0] = strconv.Itoa(i + 1)
		tmpDat[2] = item.Ip
		tmpDat[3] = tmpSystemUserStr
		p.tableWriter.DataBulk = append(p.tableWriter.DataBulk, tmpDat)
	}
	// table writer 空白空间占用宽度 4 + (columnNum - 1) * 4
	width, _ := p.term.GetSize()
	remainSize := width - 16 - IDSize - HostNameSize - IPColumnSize - systemUserSize
	if remainSize > 0 && CommentSize < remainSize {
		CommentSize = remainSize
	}
	for i, item := range p.currentData {
		if len(item.Comment) > CommentSize {
			p.tableWriter.DataBulk[i][4] = item.Comment[:CommentSize]
		} else {
			p.tableWriter.DataBulk[i][4] = item.Comment
		}
	}
	currentCapMsg := fmt.Sprintf("Page: %d, Count: %d, Total Page: %d, Total Count:%d\n",
		p.page.CurrentPage(), p.page.GetPageSize(), p.page.TotalPage(), p.page.TotalCount())
	msg := utils.WrapperString(currentCapMsg, utils.Green)
	p.tableWriter.SetCaption(msg)
	p.tableWriter.SetColumnSize(IDSize, HostNameSize, IPColumnSize, systemUserSize, CommentSize)

}

func (p *AssetPagination) PaginationState() []model.Asset {
	if !p.page.HasNextPage() {
		p.displayAssets()
		return []model.Asset{}
	}

	for {
		p.displayAssets()
		p.displayTipsInfo()
		line, err := p.term.ReadLine()
		p.setPageSize()
		if err != nil {
			return []model.Asset{}
		}
		line = strings.TrimSpace(line)
		switch len(line) {
		case 0, 1:
			switch strings.ToLower(line) {
			case "p":
				tmpData := p.page.GetPrePageData()
				if len(p.currentData) != len(tmpData) {
					p.currentData = make([]model.Asset, len(tmpData))
				}
				for i, item := range tmpData {
					p.currentData[i] = item.(model.Asset)
				}

			case "", "n":
				tmpData := p.page.GetNextPageData()
				if len(p.currentData) != len(tmpData) {
					p.currentData = make([]model.Asset, len(tmpData))
				}
				for i, item := range tmpData {
					p.currentData[i] = item.(model.Asset)
				}
			case "b":
				return []model.Asset{}
			}
		}
		if indexID, err := strconv.Atoi(line); err == nil {
			if indexID > 0 && indexID <= p.page.TotalCount() {
				return []model.Asset{p.currentData[indexID-1]}
			}
		}

		if p.page.CurrentPage() == 1 && p.page.GetPageSize() > len(p.currentData) {
			p.displayAssets()
			return []model.Asset{}
		}
	}
}

func (p *AssetPagination) displayAssets() {
	p.getColumnMaxSize()

	_, _ = p.term.Write([]byte(utils.CharClear))
	_, _ = p.term.Write([]byte(p.tableWriter.Display()))
}

func (p *AssetPagination) displayTipsInfo() {
	tips := []string{
		"\nTips: Enter the asset ID and log directly into the asset.\n",
		"\nPage up: P/p	Page down: Enter|N/n	BACK: b.\n",
	}
	for _, tip := range tips {
		_, _ = p.term.Write([]byte(tip))
	}

}

type WrapperTable struct {
	Fields     []string
	DataBulk   [][]string
	ColumnSize []int
	Caption    string
}

func (w *WrapperTable) SetColumnSize(columnSizes ...int) {
	if len(columnSizes) != len(w.Fields) {
		panic("fields' number could not match column size")
	}

	for i, size := range columnSizes {
		w.ColumnSize[i] = size
	}
}

func (w *WrapperTable) SetCaption(cap string) {
	w.Caption = cap
}

func (w *WrapperTable) Display() string {
	tableString := &strings.Builder{}
	table := tablewriter.NewWriter(tableString)
	table.SetBorder(false)
	table.SetHeader(w.Fields)
	colors := make([]tablewriter.Colors, len(w.Fields))
	for i := 0; i < len(w.Fields); i++ {
		colors[i] = tablewriter.Colors{tablewriter.Bold, tablewriter.FgGreenColor}
	}
	table.SetHeaderColor(colors...)
	table.AppendBulk(w.DataBulk)
	table.SetHeaderAlignment(tablewriter.ALIGN_LEFT)
	table.SetAlignment(tablewriter.ALIGN_LEFT)
	for i, value := range w.ColumnSize {
		table.SetColMinWidth(i, value)
	}
	if w.Caption != "" {
		table.SetCaption(true, w.Caption)
	}
	table.Render()
	return tableString.String()
}

type Pagination struct {
	data        []interface{}
	currentPage int
	pageSize    int
	totalPage   int
	lock        *sync.RWMutex
}

func (p *Pagination) GetNextPageData() []interface{} {
	if p.HasNextPage() {
		p.lock.Lock()
		p.currentPage++
		p.lock.Unlock()
	}
	return p.GetPageData(p.currentPage)

}

func (p *Pagination) GetPrePageData() []interface{} {
	if p.HasPrePage() {
		p.lock.Lock()
		p.currentPage--
		p.lock.Unlock()
	}

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
