package handler

import (
	"fmt"
	"strconv"
	"strings"

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

type AssetPagination struct {
	term        *utils.Terminal
	CurrentPage int
	TotalPage   int
	PageSize    int
	TotalNumber int
	Data        []model.Asset

	dataBulk   [][]string
	columnSize [5]int
}

func (p *AssetPagination) Initial() {
	var (
		pageSize  int
		totalPage int
	)
	_, height := p.term.GetSize()
	switch config.Conf.AssetListPageSize {
	case "auto":
		pageSize = height - 7
	case "all":
		pageSize = p.TotalNumber
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

	if p.TotalNumber%pageSize == 0 {
		totalPage = p.TotalNumber / pageSize
	} else {
		totalPage = p.TotalNumber/pageSize + 1
	}

	p.CurrentPage = 1
	p.PageSize = pageSize
	p.TotalPage = totalPage
	p.dataBulk = make([][]string, 0)

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
		if p.PageSize != pageSize {
			p.PageSize = pageSize
			p.CurrentPage = 1
			if p.TotalNumber%pageSize == 0 {
				p.TotalPage = p.TotalNumber / pageSize
			} else {
				p.TotalPage = p.TotalNumber/pageSize + 1
			}
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
	p.setPageSize()
	IDSize = IDColumnMinSize
	CommentSize = CommentColumnMinSize
	endIndex := p.CurrentPage * p.PageSize
	startIndex := endIndex - p.PageSize
	if endIndex > len(p.Data) {
		endIndex = len(p.Data)
	}
	if len(strconv.Itoa(endIndex)) > IDColumnMinSize {
		IDSize = len(strconv.Itoa(endIndex))
	}
	p.dataBulk = p.dataBulk[:0]
	for i, item := range p.Data[startIndex:endIndex] {
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
		tmpDat[0] = strconv.Itoa(startIndex + i + 1)
		tmpDat[2] = item.Ip
		tmpDat[3] = tmpSystemUserStr
		p.dataBulk = append(p.dataBulk, tmpDat)
	}
	// table writer 空白空间占用宽度 4 + (columnNum - 1) * 4
	width, _ := p.term.GetSize()
	remainSize := width - 16 - IDSize - HostNameSize - IPColumnSize - systemUserSize
	if remainSize > 0 && CommentSize < remainSize {
		CommentSize = remainSize
	}
	for i, item := range p.Data[startIndex:endIndex] {
		if len(item.Comment) > CommentSize {
			p.dataBulk[i][4] = item.Comment[:CommentSize]
		} else {
			p.dataBulk[i][4] = item.Comment
		}
	}
	p.columnSize = [5]int{IDSize, HostNameSize, IPColumnSize, systemUserSize, CommentSize}
	fmt.Println(p.columnSize)
}

func (p *AssetPagination) PaginationState() []model.Asset {
	done := make(chan struct{})
	defer close(done)
	if p.PageSize > p.TotalNumber {
		p.displayAssets()
		return []model.Asset{}
	}

	for {
		p.displayAssets()
		p.displayTipsInfo()
		line, err := p.term.ReadLine()
		if err != nil {
			return []model.Asset{}
		}
		line = strings.TrimSpace(line)
		switch len(line) {
		case 0, 1:
			switch strings.ToLower(line) {
			case "p":
				p.CurrentPage--
				if p.CurrentPage <= 0 {
					p.CurrentPage = 1
				}
				continue

			case "", "n":
				p.CurrentPage++
				if p.CurrentPage >= p.TotalPage {
					p.CurrentPage = p.TotalPage
				}
				continue
			case "b":
				return []model.Asset{}
			}
		}
		if indexID, err := strconv.Atoi(line); err == nil {
			if indexID > 0 && indexID <= p.TotalNumber {
				return []model.Asset{p.Data[indexID-1]}
			}
		}
	}
}

func (p *AssetPagination) displayAssets() {
	p.getColumnMaxSize()

	_, _ = p.term.Write([]byte(utils.CharClear))

	table := tablewriter.NewWriter(p.term)
	table.SetHeader([]string{"ID", "Hostname", "IP", "LoginAs", "Comment"})
	table.AppendBulk(p.dataBulk)
	table.SetBorder(false)
	greens := tablewriter.Colors{tablewriter.Normal, tablewriter.FgGreenColor}
	table.SetHeaderColor(greens, greens, greens, greens, greens)
	table.SetHeaderAlignment(tablewriter.ALIGN_LEFT)
	table.SetAlignment(tablewriter.ALIGN_LEFT)
	for i, value := range p.columnSize {
		table.SetColMinWidth(i, value)
	}
	currentCapMsg := fmt.Sprintf("Page: %d, Count: %d, Total Page: %d, Total Count:%d\n",
		p.CurrentPage, p.PageSize, p.TotalPage, p.TotalNumber)
	table.SetCaption(true, utils.WrapperString(currentCapMsg, utils.Green))
	table.Render()
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

type Pagination interface {
	GetNextPageData() []interface{}
	GetPrePageData() []interface{}
	GetPageData(p int) []interface{}
	CurrentPage() int
	TotalCount() int
	TotalPage() int
	SetPageSize(int)
	GetPageSize() int
}
