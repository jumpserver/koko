package handler

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/jumpserver/koko/pkg/common"
	"github.com/jumpserver/koko/pkg/config"
	"github.com/jumpserver/koko/pkg/i18n"
	"github.com/jumpserver/koko/pkg/model"
	"github.com/jumpserver/koko/pkg/service"
	"github.com/jumpserver/koko/pkg/utils"
)

func NewAssetPagination(term *utils.Terminal, assets []model.Asset) *AssetPagination {
	assetPage := &AssetPagination{term: term, assets: assets}
	assetPage.Initial()
	return assetPage
}

type AssetPagination struct {
	term        *utils.Terminal
	assets      []model.Asset
	page        *common.Pagination
	currentData []model.Asset
}

func (p *AssetPagination) Initial() {
	pageData := make([]interface{}, len(p.assets))
	for i, v := range p.assets {
		pageData[i] = v
	}
	pageSize := p.getPageSize()
	p.page = common.NewPagination(pageData, pageSize)
	firstPageData := p.page.GetPageData(1)
	p.currentData = make([]model.Asset, len(firstPageData))
	for i, item := range firstPageData {
		p.currentData[i] = item.(model.Asset)
	}
}

func (p *AssetPagination) getPageSize() int {
	var (
		pageSize  int
		minHeight = 8 // 分页显示的最小高度
	)
	_, height := p.term.GetSize()
	switch config.GetConf().AssetListPageSize {
	case "auto":
		pageSize = height - minHeight
	case "all":
		pageSize = len(p.assets)
	default:
		if value, err := strconv.Atoi(config.GetConf().AssetListPageSize); err == nil {
			pageSize = value
		} else {
			pageSize = height - minHeight
		}
	}
	if pageSize <= 0 {
		pageSize = 1
	}
	return pageSize
}

func (p *AssetPagination) Start() []model.Asset {
	p.term.SetPrompt(": ")
	defer p.term.SetPrompt("Opt> ")
	for {
		// 总数据小于page size，则显示所有资产且退出
		if p.page.PageSize() >= p.page.TotalCount() {
			p.currentData = p.assets
			p.displayPageAssets()
			return []model.Asset{}
		}

		p.displayPageAssets()
		p.displayTipsInfo()
		line, err := p.term.ReadLine()
		if err != nil {
			return []model.Asset{}
		}
		pageSize := p.getPageSize()
		p.page.SetPageSize(pageSize)

		line = strings.TrimSpace(line)
		switch len(line) {
		case 0, 1:
			switch strings.ToLower(line) {
			case "p":
				if !p.page.HasPrev() {
					continue
				}
				prePageData := p.page.GetPrevPageData()
				if len(p.currentData) != len(prePageData) {
					p.currentData = make([]model.Asset, len(prePageData))
				}
				for i, item := range prePageData {
					p.currentData[i] = item.(model.Asset)
				}

			case "", "n":
				if !p.page.HasNext() {
					continue
				}
				nextPageData := p.page.GetNextPageData()
				if len(p.currentData) != len(nextPageData) {
					p.currentData = make([]model.Asset, len(nextPageData))
				}
				for i, item := range nextPageData {
					p.currentData[i] = item.(model.Asset)
				}
			case "b", "q":
				return []model.Asset{}
			default:
				if indexID, err := strconv.Atoi(line); err == nil {
					if indexID > 0 && indexID <= len(p.currentData) {
						return []model.Asset{p.currentData[indexID-1]}
					}
				}
			}
		default:
			if indexID, err := strconv.Atoi(line); err == nil {
				if indexID > 0 && indexID <= len(p.currentData) {
					return []model.Asset{p.currentData[indexID-1]}
				}
			}
		}
	}
}

func (p *AssetPagination) displayPageAssets() {
	Labels := []string{i18n.T("ID"), i18n.T("hostname"), i18n.T("IP"), i18n.T("comment")}
	fields := []string{"ID", "hostname", "IP", "comment"}
	data := make([]map[string]string, len(p.currentData))
	for i, j := range p.currentData {
		row := make(map[string]string)
		row["ID"] = strconv.Itoa(i + 1)
		row["hostname"] = j.Hostname
		row["IP"] = j.IP

		comments := make([]string, 0)
		for _, item := range strings.Split(strings.TrimSpace(j.Comment), "\r\n") {
			if strings.TrimSpace(item) == "" {
				continue
			}
			comments = append(comments, strings.ReplaceAll(strings.TrimSpace(item), " ", ","))
		}
		row["comment"] = strings.Join(comments, "|")
		data[i] = row
	}
	w, _ := p.term.GetSize()
	caption := fmt.Sprintf(i18n.T("Page: %d, Count: %d, Total Page: %d, Total Count: %d"),
		p.page.CurrentPage(), p.page.PageSize(), p.page.TotalPage(), p.page.TotalCount(),
	)
	caption = utils.WrapperString(caption, utils.Green)
	table := common.WrapperTable{
		Fields: fields,
		Labels: Labels,
		FieldsSize: map[string][3]int{
			"ID":       {0, 0, 5},
			"hostname": {0, 8, 0},
			"IP":       {0, 15, 40},
			"comment":  {0, 0, 0},
		},
		Data:        data,
		TotalSize:   w,
		Caption:     caption,
		TruncPolicy: common.TruncMiddle,
	}
	table.Initial()

	_, _ = p.term.Write([]byte(utils.CharClear))
	_, _ = p.term.Write([]byte(table.Display()))
}

func (p *AssetPagination) displayTipsInfo() {
	tips := []string{
		i18n.T("\nTips: Enter the asset ID and log directly into the asset.\n"),
		i18n.T("\nPage up: P/p	Page down: Enter|N/n	BACK: b.\n"),
	}
	for _, tip := range tips {
		_, _ = p.term.Write([]byte(tip))
	}

}

func NewUserPagination(term *utils.Terminal, uid, search string, proxy bool) *UserAssetPagination {
	return &UserAssetPagination{
		UserID: uid,
		offset: 0,
		limit:  0,
		search: search,
		term:   term,
		proxy:  proxy,
		Data:   model.AssetsPaginationResponse{},
	}
}

type UserAssetPagination struct {
	UserID string
	offset int
	limit  int
	search string
	term   *utils.Terminal
	proxy  bool
	Data   model.AssetsPaginationResponse
}

func (p *UserAssetPagination) Start() []model.Asset {
	p.term.SetPrompt(": ")
	defer p.term.SetPrompt("Opt> ")
	for {
		p.retrieveData()

		if p.proxy && p.Data.Total == 1 {
			return p.Data.Data
		}

		// 无上下页，则退出循环
		if p.Data.NextURL == "" && p.Data.PreviousURL == "" {
			p.displayPageAssets()
			return p.Data.Data
		}

	inLoop:
		p.displayPageAssets()
		p.displayTipsInfo()
		line, err := p.term.ReadLine()
		if err != nil {
			return p.Data.Data
		}

		line = strings.TrimSpace(line)
		switch len(line) {
		case 0, 1:
			switch strings.ToLower(line) {
			case "p":
				if p.Data.PreviousURL == "" {
					continue
				}
				p.offset -= p.limit
			case "", "n":
				if p.Data.NextURL == "" {
					continue
				}
				p.offset += p.limit
			case "b", "q":
				return []model.Asset{}
			default:
				if indexID, err := strconv.Atoi(line); err == nil {
					if indexID > 0 && indexID <= len(p.Data.Data) {
						return []model.Asset{p.Data.Data[indexID-1]}
					}
				}
				goto inLoop
			}
		default:
			if indexID, err := strconv.Atoi(line); err == nil {
				if indexID > 0 && indexID <= len(p.Data.Data) {
					return []model.Asset{p.Data.Data[indexID-1]}
				}
			}
			goto inLoop
		}
	}
}

func (p *UserAssetPagination) displayPageAssets() {
	if len(p.Data.Data) == 0 {
		_, _ = p.term.Write([]byte(i18n.T("No Assets")))
		_, _ = p.term.Write([]byte("\n\r"))
		return
	}

	Labels := []string{i18n.T("ID"), i18n.T("hostname"), i18n.T("IP"), i18n.T("comment")}
	fields := []string{"ID", "hostname", "IP", "comment"}
	data := make([]map[string]string, len(p.Data.Data))
	for i, j := range p.Data.Data {
		row := make(map[string]string)
		row["ID"] = strconv.Itoa(i + 1)
		row["hostname"] = j.Hostname
		row["IP"] = j.IP

		comments := make([]string, 0)
		for _, item := range strings.Split(strings.TrimSpace(j.Comment), "\r\n") {
			if strings.TrimSpace(item) == "" {
				continue
			}
			comments = append(comments, strings.ReplaceAll(strings.TrimSpace(item), " ", ","))
		}
		row["comment"] = strings.Join(comments, "|")
		data[i] = row
	}
	w, _ := p.term.GetSize()
	var pageSize int
	var totalPage int
	var currentPage int
	var totalCount int
	var currentOffset int
	currentOffset = p.offset + len(p.Data.Data)
	switch p.limit {
	case 0:
		pageSize = len(p.Data.Data)
		totalCount = pageSize
		totalPage = 1
		currentPage = 1
	default:
		pageSize = p.limit
		totalCount = p.Data.Total

		switch totalCount % pageSize {
		case 0:
			totalPage = totalCount / pageSize
		default:
			totalPage = (totalCount / pageSize) + 1
		}
		switch currentOffset % pageSize {
		case 0:
			currentPage = currentOffset / pageSize
		default:
			currentPage = (currentOffset / pageSize) + 1
		}
	}
	caption := fmt.Sprintf(i18n.T("Page: %d, Count: %d, Total Page: %d, Total Count: %d"),
		currentPage, pageSize, totalPage, totalCount,
	)
	caption = utils.WrapperString(caption, utils.Green)
	table := common.WrapperTable{
		Fields: fields,
		Labels: Labels,
		FieldsSize: map[string][3]int{
			"ID":       {0, 0, 5},
			"hostname": {0, 8, 0},
			"IP":       {0, 15, 40},
			"comment":  {0, 0, 0},
		},
		Data:        data,
		TotalSize:   w,
		Caption:     caption,
		TruncPolicy: common.TruncMiddle,
	}
	table.Initial()

	_, _ = p.term.Write([]byte(utils.CharClear))
	_, _ = p.term.Write([]byte(table.Display()))
}

func (p *UserAssetPagination) displayTipsInfo() {
	tips := []string{
		i18n.T("\nTips: Enter the asset ID and log directly into the asset.\n"),
		i18n.T("\nPage up: P/p	Page down: Enter|N/n	BACK: b.\n"),
	}
	for _, tip := range tips {
		_, _ = p.term.Write([]byte(tip))
	}

}

func (p *UserAssetPagination) retrieveData() {
	p.limit = GetPageSize(p.term)
	if p.limit == 0 || p.offset < 0 || p.limit >= p.Data.Total {
		p.offset = 0
	}
	p.Data = service.GetUserAssets(p.UserID, p.search, p.limit, p.offset)
}

func GetPageSize(term *utils.Terminal) int {
	var (
		pageSize  int
		minHeight = 8 // 分页显示的最小高度

	)
	_, height := term.GetSize()
	conf := config.GetConf()
	switch conf.AssetListPageSize {
	case "auto":
		pageSize = height - minHeight
	case "all":
		return 0
	default:
		if value, err := strconv.Atoi(conf.AssetListPageSize); err == nil {
			pageSize = value
		} else {
			pageSize = height - minHeight
		}
	}
	if pageSize <= 0 {
		pageSize = 1
	}
	return pageSize
}
